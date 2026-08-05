package services

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const askpassPasswordFileEnv = "LAZYSSH_ASKPASS_PASSWORD_FILE"

// HandleSSHAskpass handles an invocation of the LazySSH executable by
// OpenSSH's SSH_ASKPASS mechanism. The password itself is passed through an
// owner-only temporary file, never through command-line arguments or an
// environment variable.
func HandleSSHAskpass(args []string, output io.Writer) (bool, error) {
	passwordPath := os.Getenv(askpassPasswordFileEnv)
	if passwordPath == "" {
		return false, nil
	}
	prompt := strings.ToLower(strings.Join(args, " "))
	if !strings.Contains(prompt, "password") && !strings.Contains(prompt, "密码") {
		return true, errors.New("refusing to provide a saved server password for a non-password SSH prompt")
	}
	password, err := os.ReadFile(passwordPath)
	if err != nil {
		return true, fmt.Errorf("read temporary SSH password: %w", err)
	}
	defer clearSecret(password)
	if len(password) == 0 {
		return true, errors.New("temporary SSH password is empty")
	}
	if _, err := output.Write(password); err != nil {
		return true, fmt.Errorf("write SSH password response: %w", err)
	}
	_, err = io.WriteString(output, "\n")
	return true, err
}

func newSSHCommand(args []string, loginPassword string) (*exec.Cmd, func(), error) {
	// #nosec G204 -- arguments are explicit SSH options and a selected local alias.
	command := exec.Command("ssh", args...)
	if loginPassword == "" {
		return command, func() {}, nil
	}
	if strings.ContainsAny(loginPassword, "\x00\r\n") {
		return nil, nil, errors.New("saved server password must not contain NUL or newline characters")
	}

	passwordFile, err := os.CreateTemp("", "lazyssh-askpass-*.password")
	if err != nil {
		return nil, nil, fmt.Errorf("create temporary SSH password file: %w", err)
	}
	passwordPath := passwordFile.Name()
	cleanup := func() { secureRemovePasswordFile(passwordPath) }
	if err := passwordFile.Chmod(0o600); err != nil {
		_ = passwordFile.Close()
		cleanup()
		return nil, nil, fmt.Errorf("secure temporary SSH password file: %w", err)
	}
	secret := []byte(loginPassword)
	_, writeErr := passwordFile.Write(secret)
	clearSecret(secret)
	closeErr := passwordFile.Close()
	if writeErr != nil || closeErr != nil {
		cleanup()
		return nil, nil, errors.Join(writeErr, closeErr)
	}

	executable, err := os.Executable()
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("resolve LazySSH executable for SSH_ASKPASS: %w", err)
	}
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = "lazyssh:0"
	}
	command.Env = replaceEnvironment(os.Environ(), map[string]string{
		"SSH_ASKPASS":          executable,
		"SSH_ASKPASS_REQUIRE":  "force",
		"DISPLAY":              display,
		askpassPasswordFileEnv: passwordPath,
	})
	return command, cleanup, nil
}

func replaceEnvironment(environment []string, values map[string]string) []string {
	result := make([]string, 0, len(environment)+len(values))
	for _, item := range environment {
		key, _, found := strings.Cut(item, "=")
		if found {
			if _, replace := values[key]; replace {
				continue
			}
		}
		result = append(result, item)
	}
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}

func secureRemovePasswordFile(path string) {
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		if file, openErr := os.OpenFile(path, os.O_WRONLY, 0); openErr == nil {
			zeros := make([]byte, info.Size())
			_, _ = file.WriteAt(zeros, 0)
			_ = file.Sync()
			_ = file.Close()
		}
	}
	_ = os.Remove(path)
}

func clearSecret(secret []byte) {
	for index := range secret {
		secret[index] = 0
	}
}

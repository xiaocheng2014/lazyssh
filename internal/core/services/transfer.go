package services

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/xiaocheng2014/lazyssh/internal/core/ports"
)

// Upload copies one explicitly selected local file or directory to a server.
// The remote destination is an absolute, exact path rather than an implicit
// directory, so the overwrite check and scp destination have the same meaning.
func (s *serverService) Upload(alias, localPath, remotePath string, options ports.TransferOptions) error {
	local, info, err := transferLocalSource(localPath, options.Recursive)
	if err != nil {
		return err
	}
	if err := validateRemoteTransferPath(remotePath); err != nil {
		return err
	}
	args, password, err := s.connectionSpec(alias)
	if err != nil {
		return err
	}
	exists, isDir, err := probeRemoteDestination(args, password, remotePath)
	if err != nil {
		return fmt.Errorf("check remote destination: %w", err)
	}
	if isDir {
		return fmt.Errorf("remote destination %q is an existing directory or symbolic link; provide a new exact path", remotePath)
	}
	if exists && (info.IsDir() || !options.Overwrite) {
		return fmt.Errorf("remote destination %q already exists; use --overwrite for a file", remotePath)
	}
	return runTransfer(args, password, local, remotePath, true, options)
}

// Download copies one remote file or directory to a local exact path.
func (s *serverService) Download(alias, remotePath, localPath string, options ports.TransferOptions) error {
	if err := validateRemoteTransferPath(remotePath); err != nil {
		return err
	}
	local, err := absoluteTransferPath(localPath)
	if err != nil {
		return err
	}
	parentInfo, err := os.Stat(filepath.Dir(local))
	if err != nil {
		return fmt.Errorf("inspect local destination parent: %w", err)
	}
	if !parentInfo.IsDir() {
		return fmt.Errorf("local destination parent is not a directory: %q", filepath.Dir(local))
	}
	if info, err := os.Lstat(local); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("local destination %q is not a regular file; choose a different exact path", local)
		}
		if options.Recursive || !options.Overwrite {
			return fmt.Errorf("local destination %q already exists; use --overwrite for a file", local)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect local destination: %w", err)
	}
	args, password, err := s.connectionSpec(alias)
	if err != nil {
		return err
	}
	return runTransfer(args, password, local, remotePath, false, options)
}

func transferLocalSource(path string, recursive bool) (string, os.FileInfo, error) {
	absolute, err := absoluteTransferPath(path)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", nil, fmt.Errorf("inspect local source: %w", err)
	}
	if info.IsDir() && !recursive {
		return "", nil, fmt.Errorf("local source %q is a directory; add --recursive", absolute)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("local source %q is not a regular file or directory", absolute)
	}
	return absolute, info, nil
}

func absoluteTransferPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" || strings.ContainsRune(path, 0) {
		return "", errors.New("local path must not be empty or contain NUL")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve local path: %w", err)
	}
	return absolute, nil
}

func validateRemoteTransferPath(path string) error {
	if !strings.HasPrefix(path, "/") || path == "/" || strings.HasSuffix(path, "/") {
		return errors.New("remote path must be an absolute, exact file or directory path (not / and without a trailing slash)")
	}
	for _, char := range path {
		if unicode.IsControl(char) {
			return errors.New("remote path must not contain control characters")
		}
	}
	return nil
}

// The probe uses distinct exit statuses so a failed SSH connection cannot be
// mistaken for an absent file. Single-quote escaping keeps paths inert in the
// remote POSIX shell, including spaces and shell metacharacters.
func probeRemoteDestination(connectionArgs []string, password, remotePath string) (exists, isDirectory bool, err error) {
	quoted := quotePOSIXShell(remotePath)
	commandText := "if test -L " + quoted + "; then exit 45; elif test -d " + quoted + "; then exit 44; elif test -e " + quoted + "; then exit 42; else exit 43; fi"
	args := append([]string(nil), connectionArgs[:len(connectionArgs)-1]...)
	args = append(args, "-o", "RemoteCommand=none", "-o", "RequestTTY=no", "-o", "ClearAllForwardings=yes", "-T", connectionArgs[len(connectionArgs)-1], commandText)
	command, cleanup, err := newSSHCommand(args, password)
	if err != nil {
		return false, false, err
	}
	command.Stdin = os.Stdin
	command.Stderr = os.Stderr
	defer cleanup()
	err = runSSHCommand(command)
	if err == nil {
		return false, false, errors.New("remote destination probe returned an unexpected success status")
	}
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) {
		switch exit.ExitCode() {
		case 42:
			return true, false, nil
		case 43:
			return false, false, nil
		case 44:
			return true, true, nil
		case 45:
			return true, true, nil // refuse symlink targets even with --overwrite
		}
	}
	return false, false, err
}

func quotePOSIXShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func runTransfer(connectionArgs []string, password, localPath, remotePath string, upload bool, options ports.TransferOptions) error {
	legacy, forceLegacyFlag := legacyTransferMode(options.Legacy)
	remote := remotePath
	if legacy {
		remote = quotePOSIXShell(remotePath)
	}
	remote = connectionArgs[len(connectionArgs)-1] + ":" + remote
	args := append([]string(nil), connectionArgs[:len(connectionArgs)-1]...)
	args = append(args, "-o", "RemoteCommand=none", "-o", "RequestTTY=no", "-o", "ClearAllForwardings=yes")
	if options.Recursive {
		args = append(args, "-r")
	}
	if forceLegacyFlag {
		args = append(args, "-O")
	}
	if upload {
		args = append(args, localPath, remote)
	} else {
		args = append(args, remote, localPath)
	}
	command, cleanup, err := newAuthenticatedCommand("scp", args, password)
	if err != nil {
		return err
	}
	defer cleanup()
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := runSSHCommand(command); err != nil {
		return fmt.Errorf("file transfer failed: %w", err)
	}
	return nil
}

var openSSHVersion = regexp.MustCompile(`OpenSSH_(?:for_Windows_)?([0-9]+)\.`)

// OpenSSH before 9 uses legacy SCP by default and does not understand -O.
// iSH uses that old client path without spawning a version probe (which could
// hit iSH's unsupported pidfd syscall).
func legacyTransferMode(requested bool) (legacy, forceFlag bool) {
	if runningOnISH() {
		return true, false
	}
	output, err := exec.Command("ssh", "-V").CombinedOutput()
	if err == nil {
		if match := openSSHVersion.FindSubmatch(output); len(match) == 2 {
			major, _ := strconv.Atoi(string(match[1]))
			if major < 9 {
				return true, false
			}
		}
	}
	return requested, requested
}

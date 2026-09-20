package services

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/xiaocheng2014/lazyssh/internal/core/ports"
)

// Upload copies one explicitly selected local file or directory to a server.
// Like scp, an existing remote directory receives the source under its
// basename. We probe that effective destination before allowing the copy.
func (s *serverService) Upload(alias, localPath, remotePath string, options ports.TransferOptions) error {
	local, info, err := transferLocalSource(localPath, options.Recursive)
	if err != nil {
		return err
	}
	if err := validateRemoteTransferPath(remotePath); err != nil {
		return err
	}
	trailingSlash := strings.HasSuffix(remotePath, "/")
	remotePath = path.Clean(remotePath)
	args, password, err := s.connectionSpec(alias)
	if err != nil {
		return err
	}
	kind, err := probeRemoteDestination(args, password, remotePath)
	if err != nil {
		return fmt.Errorf("check remote destination: %w", err)
	}
	if trailingSlash && kind != remoteDirectory {
		return fmt.Errorf("remote destination %q ends with / but is not an existing directory", remotePath)
	}
	if kind == remoteDirectory {
		effectivePath := path.Join(remotePath, filepath.Base(local))
		kind, err = probeRemoteDestination(args, password, effectivePath)
		if err != nil {
			return fmt.Errorf("check remote destination %q: %w", effectivePath, err)
		}
		if err := checkUploadTarget(kind, info.IsDir(), options.Overwrite, effectivePath); err != nil {
			return err
		}
	} else {
		if err := checkUploadTarget(kind, info.IsDir(), options.Overwrite, remotePath); err != nil {
			return err
		}
	}
	return runTransfer(args, password, local, remotePath, true, options)
}

func checkUploadTarget(kind remoteEntryKind, sourceIsDirectory, overwrite bool, target string) error {
	switch kind {
	case remoteMissing:
		return nil
	case remoteRegularFile:
		if sourceIsDirectory {
			return fmt.Errorf("remote destination %q is a file and cannot receive a directory", target)
		}
		if !overwrite {
			return fmt.Errorf("remote destination %q already exists; use --overwrite for a file", target)
		}
		return nil
	case remoteDirectory:
		return fmt.Errorf("remote destination %q is an existing directory; directory merging is not supported", target)
	case remoteSymlink:
		return fmt.Errorf("remote destination %q is a symbolic link; choose a different path", target)
	default:
		return fmt.Errorf("remote destination %q is not a regular file or directory", target)
	}
}

// Download copies one remote file or directory to a local exact path or into
// an existing local directory, matching scp's destination semantics.
func (s *serverService) Download(alias, remotePath, localPath string, options ports.TransferOptions) error {
	if err := validateRemoteTransferPath(remotePath); err != nil {
		return err
	}
	trailingSlash := strings.HasSuffix(localPath, string(os.PathSeparator)) || (os.PathSeparator == '\\' && strings.HasSuffix(localPath, "/"))
	local, err := absoluteTransferPath(localPath)
	if err != nil {
		return err
	}
	info, err := os.Lstat(local)
	if err == nil && info.IsDir() {
		name := path.Base(path.Clean(remotePath))
		if name == "/" || name == "." {
			return fmt.Errorf("cannot copy remote root into a local directory; choose an exact destination path")
		}
		if err := checkLocalDownloadTarget(filepath.Join(local, name), options); err != nil {
			return err
		}
	} else {
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect local destination: %w", err)
		}
		if trailingSlash {
			return fmt.Errorf("local destination %q ends with a separator but is not an existing directory", local)
		}
		if err := checkLocalDownloadTarget(local, options); err != nil {
			return err
		}
	}
	parentInfo, err := os.Stat(filepath.Dir(local))
	if err != nil {
		return fmt.Errorf("inspect local destination parent: %w", err)
	}
	if !parentInfo.IsDir() {
		return fmt.Errorf("local destination parent is not a directory: %q", filepath.Dir(local))
	}
	args, password, err := s.connectionSpec(alias)
	if err != nil {
		return err
	}
	return runTransfer(args, password, local, remotePath, false, options)
}

func checkLocalDownloadTarget(local string, options ports.TransferOptions) error {
	info, err := os.Lstat(local)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect local destination: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("local destination %q is not a regular file; choose a different path", local)
	}
	if options.Recursive || !options.Overwrite {
		return fmt.Errorf("local destination %q already exists; use --overwrite for a file", local)
	}
	return nil
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
	if path == "~" || strings.HasPrefix(path, "~/") || (os.PathSeparator == '\\' && strings.HasPrefix(path, "~\\")) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home directory: %w", err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve local path: %w", err)
	}
	return absolute, nil
}

func validateRemoteTransferPath(remotePath string) error {
	if !strings.HasPrefix(remotePath, "/") {
		return errors.New("remote path must be an absolute file or directory path")
	}
	for _, char := range remotePath {
		if unicode.IsControl(char) {
			return errors.New("remote path must not contain control characters")
		}
	}
	return nil
}

type remoteEntryKind uint8

const (
	remoteMissing remoteEntryKind = iota
	remoteRegularFile
	remoteDirectory
	remoteSymlink
	remoteOther
)

// The probe uses distinct exit statuses so a failed SSH connection cannot be
// mistaken for an absent file. Single-quote escaping keeps paths inert in the
// remote POSIX shell, including spaces and shell metacharacters.
func probeRemoteDestination(connectionArgs []string, password, remotePath string) (remoteEntryKind, error) {
	quoted := quotePOSIXShell(remotePath)
	commandText := "if test -L " + quoted + "; then exit 45; elif test -d " + quoted + "; then exit 44; elif test -f " + quoted + "; then exit 42; elif test -e " + quoted + "; then exit 46; else exit 43; fi"
	args := append([]string(nil), connectionArgs[:len(connectionArgs)-1]...)
	args = append(args, "-o", "RemoteCommand=none", "-o", "RequestTTY=no", "-o", "ClearAllForwardings=yes", "-T", connectionArgs[len(connectionArgs)-1], commandText)
	command, cleanup, err := newSSHCommand(args, password)
	if err != nil {
		return remoteMissing, err
	}
	command.Stdin = os.Stdin
	command.Stderr = os.Stderr
	defer cleanup()
	err = runSSHCommand(command)
	if err == nil {
		return remoteMissing, errors.New("remote destination probe returned an unexpected success status")
	}
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) {
		switch exit.ExitCode() {
		case 42:
			return remoteRegularFile, nil
		case 43:
			return remoteMissing, nil
		case 44:
			return remoteDirectory, nil
		case 45:
			return remoteSymlink, nil
		case 46:
			return remoteOther, nil
		}
	}
	return remoteMissing, err
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

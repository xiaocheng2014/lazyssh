//go:build linux

package services

import (
	"fmt"
	"syscall"
)

type ishProcessExitError struct{ code int }

func (e ishProcessExitError) Error() string {
	return fmt.Sprintf("process exited with status %d", e.code)
}
func (e ishProcessExitError) ExitCode() int { return e.code }

// runISHProcess uses the traditional fork+exec path directly. Unlike
// os.StartProcess it does not probe pidfd_open, and unlike replacing the Go
// process with execve, the child has no sysmon thread that can race iSH while
// it closes CLOEXEC file descriptors.
func runISHProcess(path string, args, environment []string, directory string) error {
	pid, err := syscall.ForkExec(path, args, &syscall.ProcAttr{
		Dir:   directory,
		Env:   environment,
		Files: []uintptr{0, 1, 2},
	})
	if err != nil {
		return err
	}

	var status syscall.WaitStatus
	for {
		_, err = syscall.Wait4(pid, &status, 0, nil)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		return err
	}
	if status.Exited() {
		if status.ExitStatus() == 0 {
			return nil
		}
		return ishProcessExitError{code: status.ExitStatus()}
	}
	if status.Signaled() {
		return fmt.Errorf("process terminated by signal %s", status.Signal())
	}
	return fmt.Errorf("process exited with unexpected wait status %d", status)
}

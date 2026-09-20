//go:build !linux

package services

import "errors"

func runISHProcess(_ string, _, _ []string, _ string) error {
	return errors.New("iSH process execution is only supported on Linux")
}

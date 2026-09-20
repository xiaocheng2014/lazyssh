package ui

import (
	"os"
	"path/filepath"
	"strings"
)

const maxLocalPathSuggestions = 100

// localPathSuggestions completes one local path without shell parsing. The
// typed prefix is preserved, including ./ and ~/, and directories end in a
// separator so users can continue into them.
func localPathSuggestions(input string) []string {
	if input == "" {
		return nil
	}
	if input == "~" {
		if _, err := os.UserHomeDir(); err == nil {
			return []string{"~" + string(os.PathSeparator)}
		}
		return nil
	}
	homePrefix := strings.HasPrefix(input, "~/") || (os.PathSeparator == '\\' && strings.HasPrefix(input, "~\\"))
	if strings.HasPrefix(input, "~") && !homePrefix {
		return nil // ~other-user is intentionally not expanded.
	}

	separator := string(os.PathSeparator)
	lastSeparator := strings.LastIndex(input, separator)
	if os.PathSeparator == '\\' {
		if slash := strings.LastIndex(input, "/"); slash > lastSeparator {
			lastSeparator = slash
		}
	}
	displayDir, prefix := "", input
	if lastSeparator >= 0 {
		displayDir = input[:lastSeparator+1]
		prefix = input[lastSeparator+1:]
	}
	readDir := displayDir
	if readDir == "" {
		readDir = "."
	} else if homePrefix {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		readDir = filepath.Join(home, readDir[2:])
	}
	if strings.HasSuffix(displayDir, "/") {
		separator = "/"
	}
	entries, err := os.ReadDir(readDir)
	if err != nil {
		return nil
	}

	var directories, files []string
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || (strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".")) {
			continue
		}
		candidate := displayDir + name
		isDirectory := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			if info, statErr := os.Stat(filepath.Join(readDir, name)); statErr == nil {
				isDirectory = info.IsDir()
			}
		}
		if isDirectory {
			directories = append(directories, candidate+separator)
		} else {
			files = append(files, candidate)
		}
		if len(directories)+len(files) >= maxLocalPathSuggestions {
			break
		}
	}
	return append(directories, files...)
}

//go:build linux

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// execPlatform replaces the current process with the command; Landlock
// restrictions survive exec.
func execPlatform(command []string) error {
	path, err := exec.LookPath(command[0])
	if err != nil {
		return err
	}
	return syscall.Exec(path, command, os.Environ())
}

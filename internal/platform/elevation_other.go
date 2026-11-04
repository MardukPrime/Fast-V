//go:build !windows

package platform

import (
	"errors"
	"os/exec"
)

func IsElevated() bool {
	return false
}

func LaunchElevatedRestart() error {
	return errors.New("administrator elevation is only supported on Windows")
}

func ConfigureHiddenProcess(cmd *exec.Cmd) {}

//go:build !windows

package proxy

import (
	"os"
	"os/exec"
)

type processJob struct{}

func newProcessJob() (*processJob, error) {
	return &processJob{}, nil
}

func (j *processJob) Assign(process *os.Process) error {
	return nil
}

func (j *processJob) Close() error {
	return nil
}

func hideWindow(cmd *exec.Cmd) {}

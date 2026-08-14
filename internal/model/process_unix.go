//go:build !windows

package model

import (
	"os"
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func processAlive(process *os.Process) bool {
	err := process.Signal(syscall.Signal(0))
	return err == nil || err == syscall.EPERM
}

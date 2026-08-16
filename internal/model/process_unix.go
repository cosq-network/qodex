//go:build !windows

package model

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func processAlive(process *os.Process) bool {
	err := process.Signal(syscall.Signal(0))
	return err == nil || err == syscall.EPERM
}

func processMatches(process *os.Process, backend Backend) bool {
	if process == nil {
		return false
	}
	output, err := exec.Command("ps", "-p", strconv.Itoa(process.Pid), "-o", "command=").Output()
	if err != nil {
		return false
	}
	command := strings.ToLower(strings.TrimSpace(string(output)))
	switch backend {
	case BackendLlamaCpp:
		return strings.Contains(command, "llama-server")
	case BackendVLLM:
		return strings.Contains(command, "vllm") && strings.Contains(command, "api_server")
	case BackendSGLang:
		return strings.Contains(command, "sglang")
	default:
		return false
	}
}

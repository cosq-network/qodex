//go:build windows

package model

import (
	"os"
	"os/exec"

	"golang.org/x/sys/windows"
)

func setProcessGroup(cmd *exec.Cmd) {}

func processAlive(process *os.Process) bool {
	if process == nil {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(process.Pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var exitCode uint32
	const stillActive = 259
	return windows.GetExitCodeProcess(handle, &exitCode) == nil && exitCode == stillActive
}

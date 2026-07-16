//go:build windows

package model

import "os/exec"

func setProcessGroup(cmd *exec.Cmd) {}

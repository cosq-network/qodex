//go:build !windows

package main

import (
	"os"
	"syscall"
)

var interruptSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

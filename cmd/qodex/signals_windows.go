//go:build windows

package main

import "os"

var interruptSignals = []os.Signal{os.Interrupt}

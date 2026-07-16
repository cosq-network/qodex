//go:build windows

package model

import (
	"context"
	"fmt"
	"os"
	"time"
)

func (m *Manager) Stop() error {
	status, err := m.Status(context.Background())
	if err != nil {
		return err
	}
	if !status.Running {
		fmt.Println("Model server not running")
		return nil
	}

	if status.PID > 0 {
		p, err := os.FindProcess(status.PID)
		if err == nil && p != nil {
			_ = p.Signal(os.Interrupt)
			time.Sleep(1 * time.Second)
			if running, _ := m.Status(context.Background()); running.Running {
				_ = p.Kill()
			}
		}
	}

	_ = m.ClearState()
	fmt.Println("Model server stopped")
	return nil
}

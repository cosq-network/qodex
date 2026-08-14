package model

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestProcessAliveTracksExitAcrossPlatforms(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestModelProcessHelper")
	cmd.Env = append(os.Environ(), "QODEX_MODEL_HELPER_PROCESS=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	if !processAlive(cmd.Process) {
		t.Fatal("expected helper process to be alive")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		// A killed process is expected to return a non-nil wait error.
		if cmd.ProcessState == nil {
			t.Fatalf("wait did not produce process state: %v", err)
		}
	}
	if processAlive(cmd.Process) {
		t.Fatal("expected helper process to be stopped")
	}
}

func TestModelProcessHelper(t *testing.T) {
	if os.Getenv("QODEX_MODEL_HELPER_PROCESS") != "1" {
		return
	}
	time.Sleep(10 * time.Second)
}

package model

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticsDetectsInstalledBackendAndModel(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	modelsDir := filepath.Join(root, "models")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "llama-server"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelsDir, "demo.gguf"), []byte("model"), 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(BackendLlamaCpp, root, "demo.gguf", 8080)
	diag := mgr.Diagnostics(context.Background())
	if !diag.BackendInstalled {
		t.Fatal("expected backend to be detected as installed")
	}
	if !diag.ModelPresent {
		t.Fatal("expected model to be detected as present")
	}
	if diag.ModelPath == "" {
		t.Fatal("expected resolved model path")
	}
}

func TestManagerSaveAndLoadState(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(BackendLlamaCpp, root, "demo.gguf", 8123)
	if err := mgr.saveState(4321); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}
	state, err := mgr.LoadState()
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if state.Port != 8123 || state.PID != 4321 {
		t.Fatalf("unexpected state: %+v", state)
	}
	if state.Endpoint != "http://127.0.0.1:8123/v1" {
		t.Fatalf("unexpected endpoint: %s", state.Endpoint)
	}
}

func TestEnsureUsablePortChoosesFreePortWhenOccupied(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	mgr := NewManager(BackendLlamaCpp, t.TempDir(), "demo.gguf", port)
	if err := mgr.ensureUsablePort(); err != nil {
		t.Fatalf("ensureUsablePort failed: %v", err)
	}
	if mgr.Port() == port {
		t.Fatal("expected manager to switch to a different free port")
	}
	if !isPortAvailable(mgr.Port()) {
		t.Fatalf("expected chosen port %d to be available", mgr.Port())
	}
}

func TestStatusUsesSavedStatePort(t *testing.T) {
	root := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	mgr := NewManager(BackendLlamaCpp, root, "demo.gguf", 8080)
	if err := mgr.saveState(1234); err != nil {
		t.Fatalf("saveState failed: %v", err)
	}
	statePath := filepath.Join(root, "run", "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	updated := string(data)
	updated = fmt.Sprintf(`{"backend":"llama.cpp","model":"demo.gguf","port":%d,"pid":1234,"endpoint":"http://127.0.0.1:%d/v1","updated_at":"now"}`, port, port)
	if err := os.WriteFile(statePath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}

	status, err := mgr.Status(context.Background())
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if status.Port != port {
		t.Fatalf("status port = %d, want %d", status.Port, port)
	}
}

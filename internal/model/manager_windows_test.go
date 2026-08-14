//go:build windows

package model

import (
	"context"
	"strings"
	"testing"
)

func TestNativeWindowsLlamaCppInstallExplainsManagedBackendLimit(t *testing.T) {
	mgr := NewManager(BackendLlamaCpp, t.TempDir(), "demo.gguf", 8080)
	err := mgr.Install(context.Background())
	if err == nil || !strings.Contains(err.Error(), "native Windows") {
		t.Fatalf("Install error = %v, want native Windows guidance", err)
	}
}

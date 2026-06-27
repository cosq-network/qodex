package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRunCommandWithFakeEndpoint(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(original); err != nil {
			t.Fatal(err)
		}
	}()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local listener unavailable: %v", err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"message": map[string]string{
					"role":    "assistant",
					"content": "smoke test ok",
				},
			}},
		})
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	qodexDir := filepath.Join(root, ".qodex")
	if err := os.MkdirAll(qodexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(qodexDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`[model]
provider = "openai-compatible"
base_url = "`+server.URL+`/v1"
model = "fake"

[runtime]
backend = "llama.cpp"
context_tokens = 4096
temperature = 0.1
top_p = 0.9

[approval]
write_files = "ask"
run_commands = "ask"
network = "ask"

[store]
path = ".qodex/qodex.db"

[agent]
max_steps = 2
`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := rootCmd()
	cmd.SetArgs([]string{"--config", configPath, "run", "say ok"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

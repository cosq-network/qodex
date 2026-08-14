package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStdioClientDiscoversAndCallsTool(t *testing.T) {
	t.Setenv("GO_WANT_MCP_HELPER_PROCESS", "1")
	client, err := Start(context.Background(), ServerConfig{Command: os.Args[0], Args: []string{"-test.run=TestMCPHelperProcess"}})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %+v", tools)
	}
	content, isError, err := client.CallTool(context.Background(), "echo", json.RawMessage(`{"message":"hello"}`))
	if err != nil || isError || content != "hello" {
		t.Fatalf("CallTool = %q, %v, %v", content, isError, err)
	}
	health, err := client.Health(context.Background())
	if err != nil || !health.Reachable || !health.Ping || health.ToolCount != 1 || health.ServerInfo.Name != "test" || health.Protocol != "2024-11-05" {
		t.Fatalf("Health = %+v, err=%v", health, err)
	}
}

func TestStartPassesConfiguredAuthWithoutLoggingSecret(t *testing.T) {
	t.Setenv("GO_WANT_MCP_HELPER_PROCESS", "1")
	t.Setenv("GO_WANT_MCP_AUTH_HELPER", "1")
	t.Setenv("MCP_TEST_TOKEN", "secret-value")
	client, err := Start(context.Background(), ServerConfig{Command: os.Args[0], Args: []string{"-test.run=TestMCPHelperProcess"}, Auth: AuthConfig{Type: "bearer", TokenEnv: "MCP_TEST_TOKEN", PassEnv: "MCP_SERVER_TOKEN"}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
}

func TestStartRejectsMissingConfiguredAuth(t *testing.T) {
	t.Setenv("GO_WANT_MCP_HELPER_PROCESS", "1")
	t.Setenv("MCP_TEST_TOKEN", "")
	_, err := Start(context.Background(), ServerConfig{Command: os.Args[0], Args: []string{"-test.run=TestMCPHelperProcess"}, Auth: AuthConfig{Type: "bearer", TokenEnv: "MCP_TEST_TOKEN"}})
	if err == nil || !strings.Contains(err.Error(), "MCP authentication token") {
		t.Fatalf("expected missing token error, got %v", err)
	}
}

func TestHTTPMessageDecoding(t *testing.T) {
	result, err := decodeHTTPMessage("application/json", []byte(`{"jsonrpc":"2.0","id":4,"result":{"tools":[]}}`), 4)
	if err != nil || string(result) != `{"tools":[]}` {
		t.Fatalf("JSON decode = %s, %v", result, err)
	}
	result, err = decodeHTTPMessage("text/event-stream", []byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":4,\"result\":{\"ok\":true}}\n\n"), 4)
	if err != nil || string(result) != `{"ok":true}` {
		t.Fatalf("SSE decode = %s, %v", result, err)
	}
}

func TestApplyHTTPAuth(t *testing.T) {
	t.Setenv("MCP_HTTP_KEY", "secret")
	headers := map[string]string{}
	if err := applyHTTPAuth(headers, AuthConfig{Type: "api_key", TokenEnv: "MCP_HTTP_KEY", Header: "X-API-Key"}); err != nil {
		t.Fatal(err)
	}
	if headers["X-API-Key"] != "secret" {
		t.Fatalf("headers = %#v", headers)
	}
}

func TestCallToolHonorsContextWhenServerDoesNotRespond(t *testing.T) {
	t.Setenv("GO_WANT_MCP_HELPER_PROCESS", "1")
	client, err := Start(context.Background(), ServerConfig{Command: os.Args[0], Args: []string{"-test.run=TestMCPHelperProcess"}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, _, err = client.CallTool(ctx, "hang", json.RawMessage(`{}`))
	if err != context.DeadlineExceeded {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("request took too long after cancellation: %s", elapsed)
	}
}

func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_MCP_HELPER_PROCESS") != "1" {
		return
	}
	defer os.Exit(0)
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for {
		var request rpcRequest
		if err := decoder.Decode(&request); err != nil {
			return
		}
		if request.ID == 0 {
			continue
		}
		var result interface{}
		switch request.Method {
		case "initialize":
			if os.Getenv("GO_WANT_MCP_AUTH_HELPER") == "1" && os.Getenv("MCP_SERVER_TOKEN") != "secret-value" {
				result = map[string]interface{}{"error": map[string]interface{}{"code": -32001, "message": "expected auth environment"}}
				break
			}
			result = map[string]interface{}{"protocolVersion": "2024-11-05", "capabilities": map[string]interface{}{"tools": map[string]interface{}{"listChanged": true}}, "serverInfo": map[string]string{"name": "test", "version": "1.0"}}
		case "ping":
			result = map[string]interface{}{}
		case "tools/list":
			result = map[string]interface{}{"tools": []map[string]interface{}{{"name": "echo", "description": "Echo text", "inputSchema": map[string]interface{}{"type": "object"}}}}
		case "tools/call":
			params, _ := request.Params.(map[string]interface{})
			if params["name"] == "hang" {
				continue
			}
			args, _ := params["arguments"].(map[string]interface{})
			result = map[string]interface{}{"content": []map[string]string{{"type": "text", "text": fmt.Sprint(args["message"])}}, "isError": false}
		default:
			result = map[string]interface{}{}
		}
		_ = encoder.Encode(map[string]interface{}{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}
}

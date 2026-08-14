package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
			result = map[string]interface{}{"protocolVersion": "2024-11-05", "capabilities": map[string]interface{}{}, "serverInfo": map[string]string{"name": "test"}}
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

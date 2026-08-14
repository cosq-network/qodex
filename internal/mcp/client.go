// Package mcp implements the small MCP client surface Qodex needs: stdio
// transport, initialization, tool discovery, and tools/call.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type ServerConfig struct {
	Command string
	Args    []string
	Env     map[string]string
}

type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	mu     sync.Mutex
	nextID int64
}

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func Start(ctx context.Context, cfg ServerConfig) (*Client, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("MCP command is required")
	}
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Env = os.Environ()
	for key, value := range cfg.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start MCP server: %w", err)
	}
	c := &Client{cmd: cmd, stdin: stdin, stdout: bufio.NewScanner(stdout)}
	c.stdout.Buffer(make([]byte, 4096), 4*1024*1024)
	if _, err := c.request(ctx, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]string{
			"name":    "qodex",
			"version": "dev",
		},
	}); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("initialize MCP server: %w", err)
	}
	if err := c.notify(ctx, "notifications/initialized", map[string]interface{}{}); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	result, err := c.request(ctx, "tools/list", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("decode tools/list: %w", err)
	}
	return payload.Tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, arguments json.RawMessage) (string, bool, error) {
	var args interface{}
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &args); err != nil {
			return "", false, fmt.Errorf("invalid MCP arguments: %w", err)
		}
	}
	result, err := c.request(ctx, "tools/call", map[string]interface{}{"name": name, "arguments": args})
	if err != nil {
		return "", false, err
	}
	var payload struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return "", false, fmt.Errorf("decode tools/call: %w", err)
	}
	var parts []string
	for _, block := range payload.Content {
		if block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n"), payload.IsError, nil
}

func (c *Client) request(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	request := rpcRequest{JSONRPC: "2.0", ID: c.nextID, Method: method, Params: params}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return nil, err
	}
	responseCh := make(chan struct {
		result json.RawMessage
		err    error
	}, 1)
	go func() {
		for c.stdout.Scan() {
			line := strings.TrimSpace(c.stdout.Text())
			if line == "" {
				continue
			}
			var response rpcResponse
			if err := json.Unmarshal([]byte(line), &response); err != nil || response.ID != request.ID {
				continue
			}
			if response.Error != nil {
				responseCh <- struct {
					result json.RawMessage
					err    error
				}{err: fmt.Errorf("MCP %s failed (%d): %s", method, response.Error.Code, response.Error.Message)}
				return
			}
			responseCh <- struct {
				result json.RawMessage
				err    error
			}{result: response.Result}
			return
		}
		if err := c.stdout.Err(); err != nil {
			responseCh <- struct {
				result json.RawMessage
				err    error
			}{err: fmt.Errorf("read MCP response: %w", err)}
			return
		}
		responseCh <- struct {
			result json.RawMessage
			err    error
		}{err: io.EOF}
	}()
	select {
	case response := <-responseCh:
		return response.result, response.err
	case <-ctx.Done():
		// The stdio reader cannot be interrupted portably. A timed-out request
		// makes this client unusable, so terminate the child and return promptly.
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		return nil, ctx.Err()
	}
}

func (c *Client) notify(ctx context.Context, method string, params interface{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	data, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	_, err = c.stdin.Write(append(data, '\n'))
	return err
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}

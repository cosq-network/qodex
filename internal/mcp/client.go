// Package mcp implements Qodex's MCP client transports, initialization, tool
// discovery, health checks, and tools/call.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type ServerConfig struct {
	Transport string
	Command   string
	Args      []string
	Endpoint  string
	Headers   map[string]string
	Env       map[string]string
	Auth      AuthConfig
}

type AuthConfig struct {
	Type     string
	TokenEnv string
	PassEnv  string
	Header   string
}

type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

type ServerInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string          `json:"protocolVersion"`
	Capabilities    json.RawMessage `json:"capabilities"`
	ServerInfo      ServerInfo      `json:"serverInfo"`
}

type Health struct {
	Reachable    bool
	Ping         bool
	ToolCount    int
	Protocol     string
	ServerInfo   ServerInfo
	Capabilities json.RawMessage
}

type Diagnostics struct {
	Transport       string
	Command         string
	ResolvedCommand string
	Endpoint        string
	AuthConfigured  bool
	Healthy         bool
	Protocol        string
	ServerInfo      ServerInfo
	Capabilities    json.RawMessage
	ToolCount       int
	Error           string
	Hint            string
	InstallHint     string
}

type Client struct {
	cmd             *exec.Cmd
	stdin           io.WriteCloser
	stdout          *bufio.Scanner
	http            *http.Client
	endpoint        string
	headers         map[string]string
	sessionID       string
	protocolVersion string
	mu              sync.Mutex
	nextID          int64
	init            InitializeResult
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
	env, err := buildEnvironment(cfg)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Env = env
	for key, value := range cfg.Env {
		cmd.Env = setEnvironment(cmd.Env, key, value)
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
	result, err := c.request(ctx, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]string{
			"name":    "qodex",
			"version": "dev",
		},
	})
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("initialize MCP server: %w", err)
	}
	if err := json.Unmarshal(result, &c.init); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("decode initialize MCP server: %w", err)
	}
	if err := c.notify(ctx, "notifications/initialized", map[string]interface{}{}); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// StartHTTP connects to an MCP Streamable HTTP endpoint. It keeps the
// negotiated protocol and session headers for subsequent requests.
func StartHTTP(ctx context.Context, cfg ServerConfig) (*Client, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("MCP HTTP endpoint is required")
	}
	if err := validateAuth(cfg.Auth); err != nil {
		return nil, err
	}
	headers := cloneHeaders(cfg.Headers)
	if err := applyHTTPAuth(headers, cfg.Auth); err != nil {
		return nil, err
	}
	c := &Client{http: &http.Client{Timeout: 30 * time.Second}, endpoint: cfg.Endpoint, headers: headers}
	result, responseHeaders, err := c.httpRequest(ctx, rpcRequest{JSONRPC: "2.0", ID: 1, Method: "initialize", Params: map[string]interface{}{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]string{
			"name":    "qodex",
			"version": "dev",
		},
	}})
	if err != nil {
		return nil, fmt.Errorf("initialize MCP HTTP server: %w", err)
	}
	if err := json.Unmarshal(result, &c.init); err != nil {
		return nil, fmt.Errorf("decode initialize MCP HTTP server: %w", err)
	}
	c.nextID = 1
	c.protocolVersion = c.init.ProtocolVersion
	c.sessionID = responseHeaders.Get("Mcp-Session-Id")
	if err := c.notify(ctx, "notifications/initialized", map[string]interface{}{}); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) ServerInfo() ServerInfo { return c.init.ServerInfo }

func (c *Client) Capabilities() json.RawMessage {
	return append(json.RawMessage(nil), c.init.Capabilities...)
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	health := Health{
		Protocol:     c.init.ProtocolVersion,
		ServerInfo:   c.init.ServerInfo,
		Capabilities: c.Capabilities(),
	}
	if _, err := c.request(ctx, "ping", map[string]interface{}{}); err != nil {
		return health, fmt.Errorf("MCP ping failed: %w", err)
	}
	health.Reachable = true
	health.Ping = true
	tools, err := c.ListTools(ctx)
	if err != nil {
		return health, fmt.Errorf("MCP tools/list failed: %w", err)
	}
	health.ToolCount = len(tools)
	return health, nil
}

func Diagnose(ctx context.Context, cfg ServerConfig) Diagnostics {
	transport := cfg.Transport
	if transport == "" {
		transport = "stdio"
	}
	diag := Diagnostics{Transport: transport, Command: cfg.Command, Endpoint: cfg.Endpoint, AuthConfigured: authConfigured(cfg.Auth)}
	if transport == "streamable-http" {
		if err := validateAuth(cfg.Auth); err != nil {
			diag.Error = err.Error()
			diag.Hint = fmt.Sprintf("Set %s in the environment before starting Qodex", cfg.Auth.TokenEnv)
			return diag
		}
		client, err := StartHTTP(ctx, cfg)
		if err != nil {
			diag.Error = err.Error()
			diag.Hint = "Verify the endpoint, authentication metadata, and MCP server availability"
			return diag
		}
		defer client.Close()
		health, err := client.Health(ctx)
		diag.Protocol, diag.ServerInfo, diag.Capabilities, diag.ToolCount = health.Protocol, health.ServerInfo, health.Capabilities, health.ToolCount
		if err != nil {
			diag.Error = err.Error()
			diag.Hint = "Verify Streamable HTTP, MCP-Protocol-Version, and session header support"
			return diag
		}
		diag.Healthy = true
		return diag
	}
	resolved, err := exec.LookPath(cfg.Command)
	if err != nil {
		diag.Error = fmt.Sprintf("MCP command %q is not installed or not on PATH", cfg.Command)
		diag.Hint = installHint(cfg.Command, cfg.Args)
		diag.InstallHint = diag.Hint
		diag.InstallHint = diag.Hint
		return diag
	}
	diag.ResolvedCommand = resolved
	diag.InstallHint = installHint(cfg.Command, cfg.Args)
	if err := validateAuth(cfg.Auth); err != nil {
		diag.Error = err.Error()
		diag.Hint = fmt.Sprintf("Set %s in the environment before starting Qodex", cfg.Auth.TokenEnv)
		return diag
	}
	client, err := Start(ctx, cfg)
	if err != nil {
		diag.Error = err.Error()
		diag.Hint = diag.InstallHint + "; check the command arguments and MCP server logs"
		return diag
	}
	defer client.Close()
	health, err := client.Health(ctx)
	diag.Protocol = health.Protocol
	diag.ServerInfo = health.ServerInfo
	diag.Capabilities = health.Capabilities
	diag.ToolCount = health.ToolCount
	if err != nil {
		diag.Error = err.Error()
		diag.Hint = "Verify that the process speaks MCP over stdio and supports initialize, ping, and tools/list"
		return diag
	}
	diag.Healthy = true
	return diag
}

func installHint(command string, args []string) string {
	lower := strings.ToLower(command + " " + strings.Join(args, " "))
	switch {
	case strings.Contains(lower, "npx") || strings.Contains(lower, "npm"):
		return "Node.js/npm server: install Node.js/npm, verify with `npm view <package>`, and update with `npm update -g <package>` or the configured npx command"
	case strings.Contains(lower, "python") || strings.Contains(lower, "pip"):
		return "Python server: use the active interpreter, inspect with `python -m pip show <package>`, and install/update with `python -m pip install -U <package>`"
	case strings.Contains(lower, "go"):
		return "Go server: install Go, verify the module with `go run <module>`, and update a binary with `go install <module>@latest`"
	default:
		return fmt.Sprintf("Install %q or configure its absolute executable path", command)
	}
}

func authConfigured(auth AuthConfig) bool {
	return auth.Type != "" && auth.Type != "none"
}

func validateAuth(auth AuthConfig) error {
	if !authConfigured(auth) {
		return nil
	}
	if strings.TrimSpace(auth.TokenEnv) == "" {
		return fmt.Errorf("MCP authentication requires a token environment variable")
	}
	if strings.TrimSpace(os.Getenv(auth.TokenEnv)) == "" {
		return fmt.Errorf("MCP authentication token environment variable %q is not set", auth.TokenEnv)
	}
	return nil
}

func buildEnvironment(cfg ServerConfig) ([]string, error) {
	if err := validateAuth(cfg.Auth); err != nil {
		return nil, err
	}
	env := append([]string(nil), os.Environ()...)
	if authConfigured(cfg.Auth) {
		passEnv := cfg.Auth.PassEnv
		if passEnv == "" {
			passEnv = cfg.Auth.TokenEnv
		}
		env = setEnvironment(env, passEnv, os.Getenv(cfg.Auth.TokenEnv))
	}
	return env, nil
}

func setEnvironment(env []string, key, value string) []string {
	prefix := key + "="
	for i, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
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
	if c.http != nil {
		result, _, err := c.httpRequestLocked(ctx, request)
		return result, err
	}
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

func (c *Client) httpRequest(ctx context.Context, request rpcRequest) (json.RawMessage, http.Header, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.httpRequestLocked(ctx, request)
}

func (c *Client) httpRequestLocked(ctx context.Context, request rpcRequest) (json.RawMessage, http.Header, error) {
	data, err := json.Marshal(request)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.protocolVersion != "" {
		req.Header.Set("MCP-Protocol-Version", c.protocolVersion)
	}
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return nil, resp.Header, err
	}
	if resp.StatusCode >= 300 {
		return nil, resp.Header, fmt.Errorf("MCP HTTP status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	result, err := decodeHTTPMessage(resp.Header.Get("Content-Type"), body, request.ID)
	return result, resp.Header, err
}

func decodeHTTPMessage(contentType string, body []byte, id int64) (json.RawMessage, error) {
	if strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			var response rpcResponse
			if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &response) != nil || response.ID != id {
				continue
			}
			if response.Error != nil {
				return nil, fmt.Errorf("MCP HTTP request failed (%d): %s", response.Error.Code, response.Error.Message)
			}
			return response.Result, nil
		}
		return nil, fmt.Errorf("MCP HTTP stream contained no response for request %d", id)
	}
	var response rpcResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode MCP HTTP response: %w", err)
	}
	if response.Error != nil {
		return nil, fmt.Errorf("MCP HTTP request failed (%d): %s", response.Error.Code, response.Error.Message)
	}
	return response.Result, nil
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
	if c.http != nil {
		return c.httpNotify(ctx, rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
	}
	_, err = c.stdin.Write(append(data, '\n'))
	return err
}

func (c *Client) httpNotify(ctx context.Context, request rpcRequest) error {
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.protocolVersion != "" {
		req.Header.Set("MCP-Protocol-Version", c.protocolVersion)
	}
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("MCP HTTP notification status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	if c.http != nil {
		if c.sessionID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint, nil)
			if err == nil {
				req.Header.Set("Mcp-Session-Id", c.sessionID)
				for key, value := range c.headers {
					req.Header.Set(key, value)
				}
				if resp, err := c.http.Do(req); err == nil {
					_ = resp.Body.Close()
				}
			}
		}
		return nil
	}
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}

func cloneHeaders(input map[string]string) map[string]string {
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func applyHTTPAuth(headers map[string]string, auth AuthConfig) error {
	if !authConfigured(auth) {
		return nil
	}
	if err := validateAuth(auth); err != nil {
		return err
	}
	token := os.Getenv(auth.TokenEnv)
	switch auth.Type {
	case "bearer", "oauth":
		headers["Authorization"] = "Bearer " + token
	case "api_key":
		header := auth.Header
		if header == "" {
			header = "X-API-Key"
		}
		headers[header] = token
	}
	return nil
}

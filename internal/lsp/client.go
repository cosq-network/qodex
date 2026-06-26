package lsp

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

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
}

type DiagnosticResult struct {
	Kind  string       `json:"kind"`
	Items []Diagnostic `json:"items"`
}

type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	stderr  strings.Builder
	mu      sync.Mutex
	nextID  int
	rootURI string
}

func pathToURI(path string) string {
	return "file://" + path
}

func NewClient(ctx context.Context, command string, args []string) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = &strings.Builder{}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("start %s: %w", command, err)
	}

	return &Client{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}, nil
}

func (c *Client) writeMessage(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	_, err = fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n%s", len(data), data)
	return err
}

func (c *Client) readMessage() (*rpcMessage, error) {
	var contentLength int
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if n, err := fmt.Sscanf(line, "Content-Length: %d", &contentLength); err == nil && n == 1 {
			// ok
		}
	}
	if contentLength <= 0 {
		return nil, fmt.Errorf("invalid content length: %d (stderr: %s)", contentLength, c.stderr.String())
	}

	body := make([]byte, contentLength)
	if _, err := io.ReadFull(c.stdout, body); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal: %w (body: %s)", err, string(body))
	}
	return &msg, nil
}

func (c *Client) doRequest(method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	c.mu.Unlock()

	req := rpcMessage{JSONRPC: "2.0", ID: &id, Method: method}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		req.Params = data
	}

	if err := c.writeMessage(req); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	for {
		msg, err := c.readMessage()
		if err != nil {
			return nil, err
		}
		if msg.ID != nil && *msg.ID == id {
			if msg.Error != nil {
				return nil, fmt.Errorf("LSP error (%d): %s", msg.Error.Code, msg.Error.Message)
			}
			return msg.Result, nil
		}
	}
}

func (c *Client) sendNotification(method string, params interface{}) error {
	req := rpcMessage{JSONRPC: "2.0", Method: method}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return err
		}
		req.Params = data
	}
	return c.writeMessage(req)
}

func (c *Client) Initialize(rootPath string) error {
	c.rootURI = pathToURI(rootPath)

	params := map[string]interface{}{
		"processId":    nil,
		"rootUri":      c.rootURI,
		"capabilities": map[string]interface{}{},
	}
	if _, err := c.doRequest("initialize", params); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if err := c.sendNotification("initialized", map[string]interface{}{}); err != nil {
		return fmt.Errorf("initialized notification: %w", err)
	}
	return nil
}

func detechLanguage(path string) string {
	idx := strings.LastIndex(path, ".")
	if idx < 0 {
		return ""
	}
	switch strings.ToLower(path[idx:]) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".js", ".jsx", ".mjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	default:
		return ""
	}
}

func (c *Client) OpenDocument(path string) error {
	abs := c.rootURI + "/" + path
	absPath := strings.TrimPrefix(abs, "file://")
	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	uri := c.rootURI + "/" + path
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri":        uri,
			"languageId": detechLanguage(path),
			"version":    1,
			"text":       string(content),
		},
	}
	return c.sendNotification("textDocument/didOpen", params)
}

func (c *Client) Diagnostics(path string) ([]Diagnostic, error) {
	uri := c.rootURI + "/" + path
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri": uri,
		},
	}
	result, err := c.doRequest("textDocument/diagnostic", params)
	if err != nil {
		return nil, err
	}

	if strings.Contains(string(result), `"relatedDocuments"`) {
		var dr struct {
			RelatedDocuments map[string]DiagnosticResult `json:"relatedDocuments"`
		}
		if err := json.Unmarshal(result, &dr); err != nil {
			return nil, fmt.Errorf("parse diagnostics: %w", err)
		}
		var all []Diagnostic
		for _, r := range dr.RelatedDocuments {
			all = append(all, r.Items...)
		}
		return all, nil
	}

	var dr DiagnosticResult
	if err := json.Unmarshal(result, &dr); err != nil {
		return nil, fmt.Errorf("parse diagnostics result: %w", err)
	}
	return dr.Items, nil
}

func (c *Client) GotoDefinition(path string, line, character int) (*Location, error) {
	uri := c.rootURI + "/" + path
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": uri},
		"position":     map[string]interface{}{"line": line, "character": character},
	}
	result, err := c.doRequest("textDocument/definition", params)
	if err != nil {
		return nil, err
	}

	return parseOneOrManyLocations(result)
}

func parseOneOrManyLocations(data json.RawMessage) (*Location, error) {
	var one Location
	if err := json.Unmarshal(data, &one); err == nil && one.URI != "" {
		return &one, nil
	}
	var many []Location
	if err := json.Unmarshal(data, &many); err == nil && len(many) > 0 {
		return &many[0], nil
	}
	return nil, fmt.Errorf("no definition found")
}

func (c *Client) FindReferences(path string, line, character int) ([]Location, error) {
	uri := c.rootURI + "/" + path
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": uri},
		"position":     map[string]interface{}{"line": line, "character": character},
		"context":      map[string]interface{}{"includeDeclaration": true},
	}
	result, err := c.doRequest("textDocument/references", params)
	if err != nil {
		return nil, err
	}

	var many []Location
	if err := json.Unmarshal(result, &many); err != nil {
		return nil, fmt.Errorf("parse references: %w", err)
	}
	return many, nil
}

func (c *Client) Close() error {
	_, err := c.doRequest("shutdown", nil)
	_ = c.sendNotification("exit", nil)
	werr := c.cmd.Wait()
	if err != nil {
		return err
	}
	return werr
}

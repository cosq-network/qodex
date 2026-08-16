package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// UnmarshalJSON accepts both forms used by OpenAI-compatible providers for
// function arguments: an object and a JSON-encoded object string. Keeping the
// normalized object in RawMessage means every tool executor can unmarshal it
// directly into its parameter struct.
func (f *ToolCallFunction) UnmarshalJSON(data []byte) error {
	var wire struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	f.Name = wire.Name
	f.Arguments = wire.Arguments

	var encoded string
	if err := json.Unmarshal(wire.Arguments, &encoded); err == nil {
		decoded := json.RawMessage(encoded)
		if !json.Valid(decoded) {
			return fmt.Errorf("tool arguments string is not valid JSON")
		}
		f.Arguments = decoded
	}
	return nil
}

type ToolSchema struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type ResponseMessage struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type Client struct {
	BaseURL    string
	Model      string
	HTTPClient *http.Client
	DebugLog   func(string, ...interface{})
	AuthType   string
	TokenEnv   string
	AuthHeader string
	authToken  string // ephemeral token supplied by setup; never persisted
}

// ListModels returns model IDs exposed by an OpenAI-compatible endpoint.
// Providers may return additional metadata, but Qodex only needs the stable
// ID used in chat completion requests.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	c.applyAuth(req)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode model list: %w", err)
	}
	models := make([]string, 0, len(payload.Data))
	seen := make(map[string]struct{}, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("provider returned no usable models")
	}
	return models, nil
}

func NewClient(baseURL, model string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Model:   model,
		HTTPClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (c *Client) SetDebugLog(fn func(string, ...interface{})) {
	c.DebugLog = fn
}

// SetAuth configures environment-backed authentication. The token itself is
// intentionally never stored in Qodex configuration or client state.
func (c *Client) SetAuth(authType, tokenEnv, header string) {
	c.AuthType = strings.TrimSpace(authType)
	c.TokenEnv = strings.TrimSpace(tokenEnv)
	c.AuthHeader = strings.TrimSpace(header)
}

// SetAuthToken supplies an ephemeral token for one process, primarily for
// setup-time provider discovery. It is intentionally not part of config.
func (c *Client) SetAuthToken(token string) {
	c.authToken = strings.TrimSpace(token)
}

func (c *Client) applyAuth(req *http.Request) {
	if c.AuthType == "" || c.AuthType == "none" || c.TokenEnv == "" {
		return
	}
	token := c.authToken
	if token == "" {
		token = os.Getenv(c.TokenEnv)
	}
	if token == "" {
		return
	}
	switch c.AuthType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+token)
	case "api_key":
		header := c.AuthHeader
		if header == "" {
			header = "X-API-Key"
		}
		req.Header.Set(header, token)
	}
}

func (c *Client) Check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/models", nil)
	if err != nil {
		return err
	}
	c.applyAuth(req)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (c *Client) ChatStream(ctx context.Context, messages []Message, temperature, topP float64) (<-chan StreamResult, error) {
	reqBody := chatRequest{
		Model:       c.Model,
		Messages:    messages,
		Temperature: temperature,
		TopP:        topP,
		Stream:      true,
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	c.applyAuth(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	ch := make(chan StreamResult, 10)
	go func() {
		defer func() { _ = resp.Body.Close() }()
		defer close(ch)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				return
			}
			var chunk streamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				if c.DebugLog != nil {
					c.DebugLog("SSE parse error: %v (data: %s)", err, truncate(data, 100))
				}
				continue
			}
			if len(chunk.Choices) > 0 {
				select {
				case ch <- StreamResult{Content: chunk.Choices[0].Delta.Content}:
				case <-ctx.Done():
					return
				}
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case ch <- StreamResult{Err: err}:
			case <-ctx.Done():
			}
		}
	}()
	return ch, nil
}

func (c *Client) DetectCapabilities(ctx context.Context) Capabilities {
	caps := Capabilities{}

	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	reqBody := chatRequest{
		Model:    c.Model,
		Messages: []Message{{Role: "user", Content: "hi"}},
		Stream:   true,
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return caps
	}
	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return caps
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	c.applyAuth(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return caps
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return caps
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			caps.Streaming = true
			return caps
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err == nil && len(chunk.Choices) > 0 {
			caps.Streaming = true
			return caps
		}
	}
	return caps
}

func (c *Client) Chat(ctx context.Context, messages []Message, temperature, topP float64) (string, error) {
	res, err := c.chatWithTools(ctx, messages, temperature, topP, nil)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(res.Content) == "" && len(res.ToolCalls) > 0 {
		// Some OpenAI-compatible providers may emit a native tool call even
		// when Qodex requested prompt-mode completion. Preserve it in the
		// prompt-mode envelope instead of silently returning an empty response.
		call := res.ToolCalls[0]
		envelope := struct {
			ToolCall struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"tool_call"`
		}{}
		envelope.ToolCall.Name = call.Function.Name
		envelope.ToolCall.Arguments = call.Function.Arguments
		encoded, marshalErr := json.Marshal(envelope)
		if marshalErr != nil {
			return "", marshalErr
		}
		return string(encoded), nil
	}
	return res.Content, nil
}

func (c *Client) ChatWithTools(ctx context.Context, messages []Message, temperature, topP float64, tools []ToolSchema) (*ResponseMessage, error) {
	return c.chatWithTools(ctx, messages, temperature, topP, tools)
}

func (c *Client) chatWithTools(ctx context.Context, messages []Message, temperature, topP float64, tools []ToolSchema) (*ResponseMessage, error) {
	reqBody := chatRequest{
		Model:       c.Model,
		Messages:    messages,
		Temperature: temperature,
		TopP:        topP,
		Stream:      false,
		Tools:       tools,
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyAuth(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var out chatResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("model returned no choices")
	}
	choice := out.Choices[0]
	res := &ResponseMessage{
		Content:   choice.Message.Content,
		ToolCalls: choice.Message.ToolCalls,
	}
	return res, nil
}

type StreamResult struct {
	Content string
	Err     error
}

type Capabilities struct {
	Streaming bool
}

type chatRequest struct {
	Model       string       `json:"model"`
	Messages    []Message    `json:"messages"`
	Temperature float64      `json:"temperature"`
	TopP        float64      `json:"top_p"`
	Stream      bool         `json:"stream"`
	Tools       []ToolSchema `json:"tools,omitempty"`
}

type chatResponseChoice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type chatResponse struct {
	Choices []chatResponseChoice `json:"choices"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

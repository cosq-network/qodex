package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
}

func NewClient(baseURL, model string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Model:   model,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

func (c *Client) Check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/models", nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
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

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	ch := make(chan StreamResult, 10)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}
			var chunk streamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) > 0 {
				content := chunk.Choices[0].Delta.Content
				select {
				case ch <- StreamResult{Content: content}:
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

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return caps
	}
	defer resp.Body.Close()

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

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
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
	Model       string         `json:"model"`
	Messages    []Message      `json:"messages"`
	Temperature float64        `json:"temperature"`
	TopP        float64        `json:"top_p"`
	Stream      bool           `json:"stream"`
	Tools       []ToolSchema   `json:"tools,omitempty"`
}

type chatResponseChoice struct {
	Index        int      `json:"index"`
	Message      Message  `json:"message"`
	FinishReason string   `json:"finish_reason"`
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

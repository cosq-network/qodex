package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []string{}})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Check(ctx); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
}

func TestClientAppliesEnvironmentBackedBearerAuth(t *testing.T) {
	t.Setenv("QODEX_TEST_PROVIDER_KEY", "secret-token")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	c.SetAuth("bearer", "QODEX_TEST_PROVIDER_KEY", "")
	if err := c.Check(context.Background()); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
}

func TestClientAppliesEphemeralAuthToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer setup-secret" {
			t.Fatalf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "test-model")
	c.SetAuth("bearer", "GROQ_API_KEY", "")
	c.SetAuthToken("setup-secret")
	if err := c.Check(context.Background()); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
}

func TestChatPreservesUnexpectedNativeToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"choices": []interface{}{
			map[string]interface{}{"message": map[string]interface{}{
				"content": nil,
				"tool_calls": []interface{}{map[string]interface{}{
					"id": "call_1", "type": "function",
					"function": map[string]interface{}{"name": "list_files", "arguments": map[string]string{"path": "."}},
				}},
			}},
		}})
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "test-model")
	got, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "inspect"}}, 0.2, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"tool_call"`) || !strings.Contains(got, `"list_files"`) {
		t.Fatalf("unexpected preserved tool call: %s", got)
	}
}

func TestClientListsModelsWithAuthentication(t *testing.T) {
	t.Setenv("QODEX_TEST_PROVIDER_KEY", "secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s, want /v1/models", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"model-b"},{"id":"model-a"},{"id":"model-b"}]}`))
	}))
	defer srv.Close()
	client := NewClient(srv.URL+"/v1", "unused")
	client.SetAuth("bearer", "QODEX_TEST_PROVIDER_KEY", "")
	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0] != "model-b" || models[1] != "model-a" {
		t.Fatalf("models = %#v", models)
	}
}

func TestClientListsToolCapableModelsWithOpenRouterFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.URL.Query().Get("supported_parameters") != "tools" {
			t.Fatalf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"openai/gpt-oss-20b"}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/v1", "")
	c.SetAuthToken("test-token")
	c.SetAuth("bearer", "TEST_OPENROUTER_KEY", "")
	models, err := c.ListToolCapableModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0] != "openai/gpt-oss-20b" {
		t.Fatalf("models = %#v", models)
	}
}

func TestOpenRouterRequestsIncludeAttributionHeaders(t *testing.T) {
	c := NewClient("https://openrouter.ai/api/v1", "test-model")
	c.SetAuth("bearer", "TEST_OPENROUTER_KEY", "")
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+"/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.applyAuth(req)
	if req.Header.Get("HTTP-Referer") != "https://github.com/cosq-network/qodex" || req.Header.Get("X-OpenRouter-Title") != "Qodex" {
		t.Fatalf("missing OpenRouter attribution headers: %#v", req.Header)
	}
}

func TestHostedModelInfoClassifiesZeroPricedModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"free/model","name":"Free","pricing":{"prompt":"0","completion":"0","request":"0","image":"0","web_search":"0","internal_reasoning":"0"}},
			{"id":"paid/model","name":"Paid","pricing":{"prompt":"0.000001","completion":"0.000002","request":"0","image":"0","web_search":"0","internal_reasoning":"0"}}
		]}`))
	}))
	defer srv.Close()

	models, err := NewClient(srv.URL, "").ListHostedModelInfo(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || !models[0].Free || models[1].Free {
		t.Fatalf("free classification = %#v", models)
	}
}

func TestCheckFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Check(ctx); err == nil {
		t.Fatal("expected Check to fail")
	}
}

func TestCheckConnectionRefused(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "test-model")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := c.Check(ctx); err == nil {
		t.Fatal("expected connection error")
	}
}

func TestChatSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Stream {
			t.Fatal("expected non-streaming request")
		}
		if req.Model != "test-model" {
			t.Fatalf("model = %q", req.Model)
		}
		resp := chatResponse{
			Choices: []chatResponseChoice{
				{Message: Message{Role: "assistant", Content: "Hello!"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	ctx := context.Background()
	result, err := c.Chat(ctx, []Message{{Role: "user", Content: "hi"}}, 0.7, 0.9)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if result != "Hello!" {
		t.Fatalf("got content = %q, want %q", result, "Hello!")
	}
}

func TestChatErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	ctx := context.Background()
	_, err := c.Chat(ctx, []Message{{Role: "user", Content: "hi"}}, 0.7, 0.9)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChatNoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{Choices: []chatResponseChoice{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	ctx := context.Background()
	_, err := c.Chat(ctx, []Message{{Role: "user", Content: "hi"}}, 0.7, 0.9)
	if err == nil {
		t.Fatal("expected error for no choices")
	}
}

func TestChatStreamSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if !req.Stream {
			t.Fatal("expected streaming request")
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("response writer does not support flushing")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		chunks := []string{"Hello", " ", "World", "!"}
		for _, chunk := range chunks {
			data, _ := json.Marshal(streamChunk{
				Choices: []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				}{
					{Delta: struct {
						Content string `json:"content"`
					}{Content: chunk}},
				},
			})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	ctx := context.Background()
	ch, err := c.ChatStream(ctx, []Message{{Role: "user", Content: "hi"}}, 0.7, 0.9)
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var got strings.Builder
	for result := range ch {
		if result.Err != nil {
			t.Fatalf("stream error: %v", result.Err)
		}
		got.WriteString(result.Content)
	}
	if got.String() != "Hello World!" {
		t.Fatalf("got %q, want %q", got.String(), "Hello World!")
	}
}

func TestChatStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	ctx := context.Background()
	_, err := c.ChatStream(ctx, []Message{{Role: "user", Content: "hi"}}, 0.7, 0.9)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDetectCapabilitiesStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(streamChunk{
			Choices: []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			}{
				{Delta: struct {
					Content string `json:"content"`
				}{Content: "hello"}},
			},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	ctx := context.Background()
	caps := c.DetectCapabilities(ctx)
	if !caps.Streaming {
		t.Fatal("expected streaming capability")
	}
}

func TestDetectCapabilitiesNoStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(chatResponse{Choices: []chatResponseChoice{}})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	ctx := context.Background()
	caps := c.DetectCapabilities(ctx)
	if caps.Streaming {
		t.Fatal("expected no streaming capability")
	}
}

func TestDetectCapabilitiesConnectionRefused(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "test-model")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	caps := c.DetectCapabilities(ctx)
	if caps.Streaming {
		t.Fatal("expected no streaming capability on connection error")
	}
}

func TestDetectCapabilitiesNonStreamingResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	ctx := context.Background()
	caps := c.DetectCapabilities(ctx)
	if caps.Streaming {
		t.Fatal("expected no streaming for non-streaming response")
	}
}

func TestDetectCapabilitiesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	ctx := context.Background()
	caps := c.DetectCapabilities(ctx)
	if caps.Streaming {
		t.Fatal("expected no streaming on HTTP error")
	}
}

func TestNewClientPreservesPath(t *testing.T) {
	c := NewClient("http://example.com/v1", "test")
	if c.BaseURL != "http://example.com/v1" {
		t.Fatalf("BaseURL = %q", c.BaseURL)
	}
}

func TestNewClientTrimsTrailingSlash(t *testing.T) {
	c := NewClient("http://example.com/v1/", "test")
	if c.BaseURL != "http://example.com/v1" {
		t.Fatalf("BaseURL = %q", c.BaseURL)
	}
}

func TestChatStreamCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Send one chunk then wait
		data, _ := json.Marshal(streamChunk{
			Choices: []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			}{
				{Delta: struct {
					Content string `json:"content"`
				}{Content: "partial "}},
			},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		// Hold the connection open
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := c.ChatStream(ctx, []Message{{Role: "user", Content: "hi"}}, 0.7, 0.9)
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var got strings.Builder
	// Read the first chunk
	result, ok := <-ch
	if !ok {
		t.Fatal("channel closed before first chunk")
	}
	if result.Err != nil {
		t.Fatalf("unexpected error on first chunk: %v", result.Err)
	}
	got.WriteString(result.Content)
	// Cancel the context and consume remaining
	cancel()
	for result := range ch {
		if result.Err != nil {
			break
		}
		got.WriteString(result.Content)
	}
	if got.String() != "partial " {
		t.Fatalf("got %q, want %q", got.String(), "partial ")
	}
}

func TestChatWithToolsContentOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if len(req.Tools) == 0 {
			t.Fatal("expected tools in request")
		}
		if req.Tools[0].Type != "function" {
			t.Fatalf("tool[0].type = %q", req.Tools[0].Type)
		}
		resp := chatResponse{
			Choices: []chatResponseChoice{
				{Message: Message{Role: "assistant", Content: "I found the answer."}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	ctx := context.Background()
	tools := []ToolSchema{{
		Type: "function",
		Function: ToolFunction{
			Name:        "read_file",
			Description: "Read a file",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}}
	res, err := c.ChatWithTools(ctx, []Message{{Role: "user", Content: "hi"}}, 0.7, 0.9, tools)
	if err != nil {
		t.Fatalf("ChatWithTools: %v", err)
	}
	if res.Content != "I found the answer." {
		t.Fatalf("content = %q", res.Content)
	}
	if len(res.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(res.ToolCalls))
	}
}

func TestChatWithToolsToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{
			Choices: []chatResponseChoice{{
				FinishReason: "tool_calls",
				Message: Message{
					Role: "assistant",
					ToolCalls: []ToolCall{{
						ID:   "call_abc123",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "read_file",
							Arguments: json.RawMessage(`{"path":"README.md"}`),
						},
					}},
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	ctx := context.Background()
	tools := []ToolSchema{{
		Type: "function",
		Function: ToolFunction{
			Name:        "read_file",
			Description: "Read a file",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}}
	res, err := c.ChatWithTools(ctx, []Message{{Role: "user", Content: "read readme"}}, 0.7, 0.9, tools)
	if err != nil {
		t.Fatalf("ChatWithTools: %v", err)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(res.ToolCalls))
	}
	tc := res.ToolCalls[0]
	if tc.Function.Name != "read_file" {
		t.Fatalf("tool name = %q", tc.Function.Name)
	}
	if string(tc.Function.Arguments) != `{"path":"README.md"}` {
		t.Fatalf("arguments = %s", string(tc.Function.Arguments))
	}
}

func TestChatWithToolsDecodesStringArguments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"README.md\"}"}}]}}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	res, err := c.ChatWithTools(context.Background(), nil, 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(res.ToolCalls[0].Function.Arguments); got != `{"path":"README.md"}` {
		t.Fatalf("arguments = %s", got)
	}
}

func TestToolCallFunctionMarshalsArgumentsAsJSONString(t *testing.T) {
	data, err := json.Marshal(ToolCallFunction{
		Name:      "read_file",
		Arguments: json.RawMessage(`{"path":"README.md"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != `{"name":"read_file","arguments":"{\"path\":\"README.md\"}"}` {
		t.Fatalf("wire tool function = %s", got)
	}
}

func TestHTTPErrorParsesRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	err := c.Check(context.Background())
	if err == nil || RetryAfter(err) != 7*time.Second {
		t.Fatalf("error=%v retry_after=%s", err, RetryAfter(err))
	}
}

func TestChatWithToolsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream error"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	ctx := context.Background()
	_, err := c.ChatWithTools(ctx, []Message{{Role: "user", Content: "hi"}}, 0.7, 0.9, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestChatStreamSSEParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		data, _ := json.Marshal(streamChunk{
			Choices: []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			}{
				{Delta: struct {
					Content string `json:"content"`
				}{Content: "Hello"}},
			},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		_, _ = fmt.Fprintf(w, "data: {invalid json}\n\n")
		flusher.Flush()

		data, _ = json.Marshal(streamChunk{
			Choices: []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			}{
				{Delta: struct {
					Content string `json:"content"`
				}{Content: " World"}},
			},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	var logs []string
	c := NewClient(srv.URL, "test-model")
	c.SetDebugLog(func(format string, args ...interface{}) {
		logs = append(logs, fmt.Sprintf(format, args...))
	})

	ctx := context.Background()
	ch, err := c.ChatStream(ctx, []Message{{Role: "user", Content: "hi"}}, 0.7, 0.9)
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var got strings.Builder
	for result := range ch {
		if result.Err != nil {
			t.Fatalf("stream error: %v", result.Err)
		}
		got.WriteString(result.Content)
	}
	if got.String() != "Hello World" {
		t.Fatalf("got %q, want %q", got.String(), "Hello World")
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
	if !strings.Contains(logs[0], "SSE parse error") {
		t.Fatalf("expected log to contain SSE parse error, got %q", logs[0])
	}
}

func TestSetDebugLog(t *testing.T) {
	var called bool
	c := NewClient("http://example.com", "test")
	c.SetDebugLog(func(format string, args ...interface{}) {
		called = true
	})
	c.DebugLog("test")
	if !called {
		t.Fatal("expected debug log to be called")
	}
}

func TestNewClientTimeout(t *testing.T) {
	c := NewClient("http://example.com", "test")
	if c.HTTPClient.Timeout != 5*time.Minute {
		t.Fatalf("expected timeout 5m, got %v", c.HTTPClient.Timeout)
	}
}

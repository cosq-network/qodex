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
		json.NewEncoder(w).Encode(map[string]interface{}{"data": []string{}})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Check(ctx); err != nil {
		t.Fatalf("Check failed: %v", err)
	}
}

func TestCheckFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
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
			Choices: []struct {
				Message Message `json:"message"`
			}{
				{Message: Message{Role: "assistant", Content: "Hello!"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
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
		w.Write([]byte("rate limited"))
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
		resp := chatResponse{Choices: []struct {
			Message Message `json:"message"`
		}{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
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
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
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
		w.Write([]byte("bad request"))
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
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
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
		json.NewEncoder(w).Encode(chatResponse{Choices: []struct {
			Message Message `json:"message"`
		}{}})
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
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
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
		fmt.Fprintf(w, "data: %s\n\n", data)
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

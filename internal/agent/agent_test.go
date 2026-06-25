package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/benoybose/locha/internal/config"
	"github.com/benoybose/locha/internal/model"
	"github.com/benoybose/locha/internal/skills"
	"github.com/benoybose/locha/internal/store"
	"github.com/benoybose/locha/internal/tools"
)

func TestParseToolCall(t *testing.T) {
	call, ok := parseToolCall(`{"tool_call":{"name":"read_file","arguments":{"path":"README.md"}}}`)
	if !ok {
		t.Fatal("expected tool call")
	}
	if call.Name != "read_file" {
		t.Fatalf("name = %q", call.Name)
	}
	if string(call.Arguments) != `{"path":"README.md"}` {
		t.Fatalf("arguments = %s", string(call.Arguments))
	}
}

func TestParseToolCallFromMarkdownFence(t *testing.T) {
	call, ok := parseToolCall("```json\n{\"tool_call\":{\"name\":\"list_files\",\"arguments\":{\"path\":\".\"}}}\n```")
	if !ok {
		t.Fatal("expected tool call")
	}
	if call.Name != "list_files" {
		t.Fatalf("name = %q", call.Name)
	}
}

func TestParseToolCallRejectsFinalText(t *testing.T) {
	if _, ok := parseToolCall("No tool is needed."); ok {
		t.Fatal("did not expect tool call")
	}
}

func TestParseToolCallDetailedReportsInvalidToolJSON(t *testing.T) {
	_, ok, err := parseToolCallDetailed(`{"tool_call":{"name":"read_file","arguments":}`)
	if ok {
		t.Fatal("did not expect parsed tool call")
	}
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestAgentLoopWithFakeModelServer(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "locha.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := writeTestFile(filepath.Join(root, "README.md"), "Locha test project\n"); err != nil {
		t.Fatal(err)
	}

	var chatCalls atomic.Int32
	client := model.NewClient("http://fake.local/v1", "fake")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/chat/completions" {
			return response(404, `{"error":"not found"}`), nil
		}
		chatCalls.Add(1)
		if chatCalls.Load() == 1 {
			return jsonResponse(map[string]interface{}{
				"choices": []map[string]interface{}{{
					"message": map[string]string{
						"role":    "assistant",
						"content": `{"tool_call":{"name":"read_file","arguments":{"path":"README.md"}}}`,
					},
				}},
			}), nil
		}
		return jsonResponse(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"message": map[string]string{
					"role":    "assistant",
					"content": "README says this is a Locha test project.",
				},
			}},
		}), nil
	})}

	cfg := config.Defaults(root)
	cfg.Model.BaseURL = "http://fake.local/v1"
	agent := New(Options{
		Config:    cfg,
		Client:    client,
		Tools:     tools.NewRegistry(root),
		Store:     db,
		Approver:  ApproverFunc(func(ApprovalRequest) bool { return true }),
		MaxSteps:  4,
		SessionID: 0,
	})
	got, err := agent.Run(context.Background(), "read the README")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Locha test project") {
		t.Fatalf("unexpected response: %q", got)
	}
	if chatCalls.Load() != 2 {
		t.Fatalf("chat calls = %d, want 2", chatCalls.Load())
	}
}

func TestAgentClassifiesNetworkCommandForApproval(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "locha.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var approvalKind string
	agent := New(Options{
		Config: config.Defaults(root),
		Tools:  tools.NewRegistry(root),
		Store:  db,
		Approver: ApproverFunc(func(req ApprovalRequest) bool {
			approvalKind = req.Kind
			return false
		}),
		SessionID: 1,
	})
	_, err = agent.executeTool(context.Background(), toolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"argv":["curl","-I","https://example.com"]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if approvalKind != "network" {
		t.Fatalf("approval kind = %q, want network", approvalKind)
	}
}

func TestAgentEmitsToolAndApprovalEvents(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "locha.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var events []Event
	agent := New(Options{
		Config: config.Defaults(root),
		Tools:  tools.NewRegistry(root),
		Store:  db,
		Approver: ApproverFunc(func(req ApprovalRequest) bool {
			return req.Kind == "write"
		}),
		Observer: ObserverFunc(func(event Event) {
			events = append(events, event)
		}),
		SessionID: 1,
	})
	_, err = agent.executeTool(context.Background(), toolCall{
		Name:      "write_file",
		Arguments: json.RawMessage(`{"path":"note.txt","content":"hello"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, event := range events {
		got = append(got, event.Type)
	}
	want := []string{"tool_requested", "approval_requested", "approval_approved", "tool_completed"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

func TestExecuteScriptRunsPreApprovedScript(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "locha.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	agent := New(Options{
		Config:    config.Defaults(root),
		Tools:     tools.NewRegistry(root),
		Store:     db,
		Approver:  ApproverFunc(func(ApprovalRequest) bool { return true }),
		SessionID: 1,
	})
	agent.selectedSkills = []skills.Skill{
		{
			Name: "project",
			Meta: skills.Metadata{
				Scripts: []skills.Script{
					{Description: "Say hello", Command: "echo hello from script", Tool: "run_command"},
				},
			},
		},
	}

	result, err := agent.executeTool(context.Background(), toolCall{
		Name:      "run_script",
		Arguments: json.RawMessage(`{"description":"Say hello"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "hello from script") {
		t.Fatalf("expected script output in result, got: %s", result)
	}
	if !strings.Contains(result, "provenance") {
		t.Fatalf("expected provenance metadata, got: %s", result)
	}
}

func TestExecuteScriptRejectsUnknownDescription(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "locha.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	agent := New(Options{
		Config:    config.Defaults(root),
		Tools:     tools.NewRegistry(root),
		Store:     db,
		Approver:  ApproverFunc(func(ApprovalRequest) bool { return true }),
		SessionID: 1,
	})
	agent.selectedSkills = []skills.Skill{
		{Name: "project", Meta: skills.Metadata{Scripts: []skills.Script{{Description: "Run tests", Command: "go test ./..."}}}},
	}

	_, err2 := agent.executeTool(context.Background(), toolCall{
		Name:      "run_script",
		Arguments: json.RawMessage(`{"description":"nonexistent"}`),
	})
	if err2 == nil {
		t.Fatal("expected error for unknown script")
	}
}

func TestExecuteScriptRejectsNoDescription(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "locha.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	agent := New(Options{
		Config:    config.Defaults(root),
		Tools:     tools.NewRegistry(root),
		Store:     db,
		Approver:  ApproverFunc(func(ApprovalRequest) bool { return true }),
		SessionID: 1,
	})

	_, err = agent.executeTool(context.Background(), toolCall{
		Name:      "run_script",
		Arguments: json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("expected error for missing description")
	}
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(v interface{}) *http.Response {
	raw, _ := json.Marshal(v)
	return response(200, string(raw))
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

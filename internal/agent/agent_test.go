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

	"github.com/benoybose/qodex/internal/config"
	"github.com/benoybose/qodex/internal/model"
	"github.com/benoybose/qodex/internal/skills"
	"github.com/benoybose/qodex/internal/store"
	"github.com/benoybose/qodex/internal/tools"
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
	db, err := store.Open(filepath.Join(root, "qodex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if err := writeTestFile(filepath.Join(root, "README.md"), "Qodex test project\n"); err != nil {
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
					"content": "README says this is a Qodex test project.",
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
	if !strings.Contains(got, "Qodex test project") {
		t.Fatalf("unexpected response: %q", got)
	}
	if chatCalls.Load() != 2 {
		t.Fatalf("chat calls = %d, want 2", chatCalls.Load())
	}
	messages, err := db.ListMessages(context.Background(), agent.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	roles := make([]string, 0, len(messages))
	for _, msg := range messages {
		roles = append(roles, msg.Role)
	}
	if !strings.Contains(strings.Join(roles, ","), "tool") {
		t.Fatalf("expected persisted native tool messages, roles=%v", roles)
	}
}

func TestNativeToolBatchUsesOneAssistantMessage(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "qodex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := writeTestFile(filepath.Join(root, "README.md"), "hello\n"); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	client := model.NewClient("http://fake.local/v1", "fake")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return jsonResponse(map[string]interface{}{
				"choices": []map[string]interface{}{{
					"message": map[string]interface{}{
						"role": "assistant",
						"tool_calls": []map[string]interface{}{
							{"id": "call_1", "type": "function", "function": map[string]interface{}{"name": "read_file", "arguments": json.RawMessage(`{"path":"README.md"}`)}},
							{"id": "call_2", "type": "function", "function": map[string]interface{}{"name": "list_files", "arguments": json.RawMessage(`{"path":"."}`)}},
						},
					},
				}},
			}), nil
		}
		return jsonResponse(map[string]interface{}{"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "done"}}}}), nil
	})}
	cfg := config.Defaults(root)
	cfg.Model.BaseURL = "http://fake.local/v1"
	cfg.Agent.ToolCalls = "native"
	a := New(Options{Config: cfg, Client: client, Tools: tools.NewRegistry(root), Store: db, MaxSteps: 3})
	if _, err := a.Run(context.Background(), "inspect"); err != nil {
		t.Fatal(err)
	}
	messages, err := db.ListMessages(context.Background(), a.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	var assistantWithTwoCalls, toolCount int
	for _, msg := range messages {
		if msg.Role == "assistant" && msg.Content == "" {
			// Native tool-call arguments are retained in memory; the store keeps
			// the assistant content and tool results, so count tool rows below.
			assistantWithTwoCalls++
		}
		if msg.Role == "tool" {
			toolCount++
		}
	}
	if toolCount != 2 {
		t.Fatalf("tool messages = %d, want 2; messages=%+v", toolCount, messages)
	}
	if assistantWithTwoCalls < 1 {
		t.Fatalf("expected persisted assistant batch message: %+v", messages)
	}
	for _, msg := range messages {
		if msg.Role == "assistant" && msg.Content == "" && !strings.Contains(msg.Metadata, "call_1") {
			t.Fatalf("native assistant metadata missing: %+v", msg)
		}
	}
	var batchCalls int
	for _, msg := range a.messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) == 2 {
			batchCalls++
		}
	}
	if batchCalls != 1 {
		t.Fatalf("assistant tool-call batches = %d, want 1; messages=%+v", batchCalls, a.messages)
	}
}

func TestSessionRefreshesSkillsForEachTurn(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "qodex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	client := model.NewClient("http://fake.local/v1", "fake")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(map[string]interface{}{"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "done"}}}}), nil
	})}
	cfg := config.Defaults(root)
	a := New(Options{
		Config: cfg, Client: client, Tools: tools.NewRegistry(root), Store: db, MaxSteps: 1,
		Skills: []skills.Skill{
			{Name: "project", Content: "# Project"},
			{Name: "go", Content: "# Go", Meta: skills.Metadata{Triggers: []string{"go"}, AllowedTools: []string{"run_tests"}}},
			{Name: "docker", Content: "# Docker", Meta: skills.Metadata{Triggers: []string{"docker"}, AllowedTools: []string{"docker_run"}}},
		},
	})
	if _, err := a.Run(context.Background(), "go test"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Run(context.Background(), "docker run"); err != nil {
		t.Fatal(err)
	}
	if len(a.selectedSkills) != 2 || a.selectedSkills[1].Name != "docker" {
		t.Fatalf("selected skills after second turn = %#v", a.selectedSkills)
	}
	if len(a.allowedTools) != 1 || a.allowedTools[0] != "docker_run" {
		t.Fatalf("allowed tools after second turn = %#v", a.allowedTools)
	}
}

func TestAgentRetriesTransientModelFailure(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "qodex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var calls atomic.Int32
	client := model.NewClient("http://fake.local/v1", "fake")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return response(503, "temporarily unavailable"), nil
		}
		return jsonResponse(map[string]interface{}{"choices": []map[string]interface{}{{"message": map[string]string{"role": "assistant", "content": "recovered"}}}}), nil
	})}
	cfg := config.Defaults(root)
	cfg.Model.BaseURL = "http://fake.local/v1"
	a := New(Options{Config: cfg, Client: client, Tools: tools.NewRegistry(root), Store: db, MaxSteps: 2})
	got, err := a.Run(context.Background(), "hello")
	if err != nil || got != "recovered" {
		t.Fatalf("result=%q err=%v", got, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("model calls = %d, want 2", calls.Load())
	}
}

func TestCompactContextStaysWithinBudget(t *testing.T) {
	a := New(Options{Config: config.Config{Runtime: config.RuntimeConfig{ContextTokens: 80}}})
	a.messages = []model.Message{{Role: "system", Content: "system instructions"}}
	for i := 0; i < 10; i++ {
		a.messages = append(a.messages, model.Message{Role: "user", Content: strings.Repeat("long context ", 40)})
	}
	a.CompactContext()
	if got := a.messageTokens(a.messages); got > 56 {
		t.Fatalf("compacted tokens = %d, want <= 56", got)
	}
}

func TestAgentLoopWithNativeToolCalls(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "qodex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if err := writeTestFile(filepath.Join(root, "README.md"), "Qodex test project\n"); err != nil {
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
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]interface{}{{
							"id":   "call_001",
							"type": "function",
							"function": map[string]interface{}{
								"name":      "read_file",
								"arguments": `{"path":"README.md"}`,
							},
						}},
					},
					"finish_reason": "tool_calls",
				}},
			}), nil
		}
		return jsonResponse(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"message": map[string]string{
					"role":    "assistant",
					"content": "README says this is a Qodex test project.",
				},
			}},
		}), nil
	})}

	cfg := config.Defaults(root)
	cfg.Model.BaseURL = "http://fake.local/v1"
	cfg.Agent.ToolCalls = "native"
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
	if !strings.Contains(got, "Qodex test project") {
		t.Fatalf("unexpected response: %q", got)
	}
	if chatCalls.Load() != 2 {
		t.Fatalf("chat calls = %d, want 2", chatCalls.Load())
	}
}

func TestAgentClassifiesNetworkCommandForApproval(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "qodex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

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
	db, err := store.Open(filepath.Join(root, "qodex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

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
	db, err := store.Open(filepath.Join(root, "qodex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

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
	db, err := store.Open(filepath.Join(root, "qodex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

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

func TestExecuteScriptRejectsAmbiguousPartialDescription(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "qodex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	a := New(Options{Config: config.Defaults(root), Client: model.NewClient("http://fake.local/v1", "fake"), Tools: tools.NewRegistry(root), Store: db, MaxSteps: 1})
	a.selectedSkills = []skills.Skill{{Name: "project", Meta: skills.Metadata{Scripts: []skills.Script{
		{Description: "Run unit tests", Command: "go test ./..."},
		{Description: "Run integration tests", Command: "go test ./integration/..."},
	}}}}
	_, err = a.executeScript(context.Background(), toolCall{Arguments: json.RawMessage(`{"description":"tests"}`)})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous script error, got %v", err)
	}
}

func TestExecuteScriptRejectsUnsupportedTool(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "qodex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	a := New(Options{Config: config.Defaults(root), Client: model.NewClient("http://fake.local/v1", "fake"), Tools: tools.NewRegistry(root), Store: db, MaxSteps: 1})
	a.selectedSkills = []skills.Skill{{Name: "project", Meta: skills.Metadata{Scripts: []skills.Script{{Description: "Build", Command: "make", Tool: "docker_run"}}}}}
	_, err = a.executeScript(context.Background(), toolCall{Arguments: json.RawMessage(`{"description":"Build"}`)})
	if err == nil || !strings.Contains(err.Error(), "unsupported tool") {
		t.Fatalf("expected unsupported tool error, got %v", err)
	}
}

func TestExecuteScriptHonorsShellApprovalPolicy(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "qodex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	cfg := config.Defaults(root)
	cfg.Approval.RunCommands = "deny"
	agent := New(Options{
		Config:    cfg,
		Tools:     tools.NewRegistry(root),
		Store:     db,
		SessionID: 1,
	})
	agent.selectedSkills = []skills.Skill{{
		Name: "project",
		Meta: skills.Metadata{Scripts: []skills.Script{{Description: "Create marker", Command: "touch marker.txt"}}},
	}}

	result, err := agent.executeTool(context.Background(), toolCall{
		Name:      "run_script",
		Arguments: json.RawMessage(`{"description":"Create marker"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "approval denied") {
		t.Fatalf("expected approval denial, got %s", result)
	}
	if _, err := os.Stat(filepath.Join(root, "marker.txt")); !os.IsNotExist(err) {
		t.Fatalf("script ran despite deny policy: %v", err)
	}
}

func TestExecuteScriptRejectsNoDescription(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "qodex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

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

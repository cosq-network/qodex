package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benoybose/locha/internal/agent"
	"github.com/benoybose/locha/internal/store"
)

func TestApprovalPromptAcceptsYes(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	reply := make(chan bool, 1)

	updated, _ := model.Update(approvalPrompt{
		req:   agent.ApprovalRequest{Kind: "write", Summary: "write_file note.txt"},
		reply: reply,
	})
	model = updated.(Model)
	if model.pending == nil {
		t.Fatal("expected pending approval")
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = updated.(Model)
	if model.pending != nil {
		t.Fatal("expected approval to clear")
	}
	select {
	case approved := <-reply:
		if !approved {
			t.Fatal("expected approval")
		}
	default:
		t.Fatal("expected approval response")
	}
	if !strings.Contains(strings.Join(model.history, "\n"), "Approved") {
		t.Fatal("expected approved history entry")
	}
}

func TestApprovalPromptAcceptsNo(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	reply := make(chan bool, 1)

	updated, _ := model.Update(approvalPrompt{
		req:   agent.ApprovalRequest{Kind: "shell", Summary: "run_command go test ./..."},
		reply: reply,
	})
	model = updated.(Model)

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	model = updated.(Model)
	select {
	case approved := <-reply:
		if approved {
			t.Fatal("expected denial")
		}
	default:
		t.Fatal("expected approval response")
	}
	if !strings.Contains(strings.Join(model.history, "\n"), "Denied") {
		t.Fatal("expected denied history entry")
	}
}

func TestRenderEventShowsToolActivity(t *testing.T) {
	got := renderEvent(agent.Event{Type: "tool_completed", Summary: "Read README.md."})
	if !strings.Contains(got, "Tool completed") {
		t.Fatalf("unexpected event rendering: %q", got)
	}
}

func TestBusyStateShowsWorking(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	model.input.SetValue("test prompt")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if !model.busy {
		t.Fatal("expected busy state after enter")
	}
	found := false
	for _, entry := range model.history {
		if strings.Contains(entry, "Working...") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected Working... in history when busy")
	}
	if cmd == nil {
		t.Fatal("expected non-nil command to run prompt")
	}
}

func TestBusyEnterIgnored(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	model.busy = true
	model.input.SetValue("ignored prompt")
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if len(model.history) > 3 {
		t.Fatal("did not expect new history entry while busy")
	}
}

func TestResumeRendersHistory(t *testing.T) {
	now := time.Now()
	messages := []store.Message{
		{Role: "user", Content: "hello", CreatedAt: now},
		{Role: "assistant", Content: "hi there", CreatedAt: now},
	}
	model := NewWithHistory(agent.New(agent.Options{}), messages)
	foundUser := false
	foundAssistant := false
	for _, entry := range model.history {
		if strings.Contains(entry, "hello") {
			foundUser = true
		}
		if strings.Contains(entry, "hi there") {
			foundAssistant = true
		}
	}
	if !foundUser {
		t.Fatal("expected user message in resume history")
	}
	if !foundAssistant {
		t.Fatal("expected assistant message in resume history")
	}
}

func TestRenderEventShowsDiffDetail(t *testing.T) {
	got := renderEvent(agent.Event{Type: "tool_requested", Effect: "write", Summary: "write_file test.txt", Detail: "--- a/test.txt\n+++ b/test.txt\n+content"})
	if !strings.Contains(got, "content") {
		t.Fatalf("expected diff detail in event rendering: %q", got)
	}
}

func TestRenderApprovalShowsDiff(t *testing.T) {
	req := agent.ApprovalRequest{Kind: "write", Summary: "write_file test.txt", Diff: "--- a/test.txt\n+++ b/test.txt\n+new content"}
	got := renderApproval(req)
	if !strings.Contains(got, "new content") {
		t.Fatalf("expected diff in approval rendering: %q", got)
	}
	if !strings.Contains(got, "y to approve") {
		t.Fatalf("expected approval help: %q", got)
	}
}

func TestErrorPanelShowsLastError(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	model.Update(eventMsg(agent.Event{Type: "tool_failed", Tool: "read_file", Error: "file not found"}))
	updated, _ := model.Update(responseMsg{prompt: "test", err: nil, text: "done"})
	model = updated.(Model)
	if model.lastErr != "" {
		t.Fatal("expected lastErr cleared after successful response")
	}
}

func TestViewShowsErrorStatus(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	model.lastErr = "connection refused"
	view := model.View()
	if !strings.Contains(view, "connection refused") {
		t.Fatalf("expected error in view: %q", view)
	}
}

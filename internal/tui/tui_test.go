package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benoybose/locha/internal/agent"
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

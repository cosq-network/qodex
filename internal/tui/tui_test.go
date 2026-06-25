package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benoybose/qodex/internal/agent"
	"github.com/benoybose/qodex/internal/store"
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

func TestBusyStateShowsSpinner(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	model.input.SetValue("test prompt")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if !model.busy {
		t.Fatal("expected busy state after enter")
	}
	view := model.View()
	if !strings.Contains(view, "Running agent") {
		t.Fatalf("expected Running agent in view: %q", view)
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

func TestExtractAutoQuery(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"no at sign", ""},
		{"@", ""},
		{"@ ", ""},
		{"@README.md", "README.md"},
		{"prefix @README.md suffix", "README.md"},
		{"@dir/file.go more", "dir/file.go"},
		{"@a b", "a"},
	}
	for _, tc := range tests {
		got := extractAutoQuery(tc.input)
		if got != tc.want {
			t.Errorf("extractAutoQuery(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestFuzzyFind(t *testing.T) {
	files := []string{"README.md", "internal/tui/tui.go", "internal/agent/agent.go", "cmd/qodex/main.go"}
	tests := []struct {
		query string
		want  int
	}{
		{"readme", 1},
		{"tui", 1},
		{"agent", 1},
		{"nonexistent", 0},
		{"go", 3},
		{"", 0},
	}
	for _, tc := range tests {
		matches := fuzzyFind(files, tc.query)
		if len(matches) != tc.want {
			t.Errorf("fuzzyFind(%q) = %d matches, want %d: %v", tc.query, len(matches), tc.want, matches)
		}
	}
}

func TestAutocompleteSelect(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	model.projectFiles = []string{"README.md", "internal/tui/tui.go", "internal/agent/agent.go"}
	model.input.SetValue("read @README")
	model.updateAutocomplete()
	if !model.autoShow {
		t.Fatal("expected autocomplete to show")
	}
	if len(model.matches) == 0 {
		t.Fatal("expected autocomplete matches")
	}
	model.matchIdx = 0
	model.selectAutocomplete()
	val := model.input.Value()
	if !strings.HasPrefix(val, "read @") {
		t.Fatalf("expected value to contain @ followed by path, got %q", val)
	}
	if model.autoShow {
		t.Fatal("expected autocomplete cleared after selection")
	}
}

func TestAutocompleteDismissWithEscape(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	model.projectFiles = []string{"README.md", "internal/tui/tui.go"}
	model.input.SetValue("@README")
	model.updateAutocomplete()
	if !model.autoShow {
		t.Fatal("expected autocomplete to show")
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.autoShow {
		t.Fatal("expected autocomplete dismissed on escape")
	}
}

func TestAutocompletePartialMatch(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	model.projectFiles = []string{"internal/tui/tui.go", "internal/tui/tui_test.go", "internal/agent/agent.go"}
	model.input.SetValue("read @tui")
	model.updateAutocomplete()
	if !model.autoShow {
		t.Fatal("expected autocomplete to show for partial match")
	}
	if len(model.matches) != 2 {
		t.Fatalf("expected 2 matches for 'tui', got %d", len(model.matches))
	}
}

func TestSpinnerShownWhenBusy(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	model.busy = true
	view := model.View()
	if !strings.Contains(view, "Running agent") {
		t.Fatalf("expected Running agent in busy view: %q", view)
	}
}

func TestContextCompactedEvent(t *testing.T) {
	evt := agent.Event{Type: "context_compacted", Summary: "Context compacted."}
	rendered := renderEvent(evt)
	if !strings.Contains(rendered, "compacted") {
		t.Fatalf("expected compacted in render: %q", rendered)
	}
}

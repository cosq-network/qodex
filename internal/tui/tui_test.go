package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/benoybose/qodex/internal/agent"
	"github.com/benoybose/qodex/internal/config"
	"github.com/benoybose/qodex/internal/model"
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

func TestEscapeCancelsWithoutQuitting(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	cancelled := false
	model.busy = true
	model.runCancel = func() { cancelled = true }
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if cmd != nil {
		t.Fatal("expected escape to cancel without quitting")
	}
	if !cancelled || model.runCancel != nil {
		t.Fatal("expected active run cancellation")
	}
}

func TestSlashCommands(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	tests := []struct {
		input         string
		handled       bool
		wantImmediate string
		wantAction    string
	}{
		{input: "/help", handled: true, wantImmediate: "Slash commands:"},
		{input: "/model", handled: true, wantImmediate: "Use /model list"},
		{input: "/model groq llama-3.1-8b-instant", handled: true, wantAction: "__slash_model:groq llama-3.1-8b-instant"},
		{input: "/model groq", handled: true, wantAction: "__slash_model:groq"},
		{input: "/model openrouter", handled: true, wantAction: "__slash_model:openrouter"},
		{input: "/plan", handled: true, wantImmediate: "Plan state"},
		{input: "/compact", handled: true, wantImmediate: "compacted"},
		{input: "/commit release notes", handled: true, wantAction: "release notes"},
		{input: "/undo HEAD~1", handled: true, wantAction: "HEAD~1"},
		{input: "/skill go-testing", handled: false},
		{input: "explain this", handled: false},
	}
	for _, tc := range tests {
		handled, immediate, action := model.handleSlashCommand(tc.input)
		if handled != tc.handled {
			t.Errorf("%q handled = %v, want %v", tc.input, handled, tc.handled)
		}
		if tc.wantImmediate != "" && !strings.Contains(immediate, tc.wantImmediate) {
			t.Errorf("%q immediate = %q, want substring %q", tc.input, immediate, tc.wantImmediate)
		}
		if tc.wantAction != "" && !strings.Contains(action, tc.wantAction) {
			t.Errorf("%q action = %q, want substring %q", tc.input, action, tc.wantAction)
		}
	}
}

func TestRunSlashModelHostedProvider(t *testing.T) {
	root := t.TempDir()
	cfg := config.Defaults(root)
	client := model.NewClient(cfg.Model.BaseURL, cfg.Model.Model)
	a := agent.New(agent.Options{Config: cfg, Client: client})

	msg := runSlashModel(a, "groq llama-3.1-8b-instant TEAM_GROQ_KEY")()
	result, ok := msg.(slashResultMsg)
	if !ok || result.err != nil {
		t.Fatalf("unexpected slash result: %#v (err=%v)", msg, result.err)
	}
	if a.Config().Model.BaseURL != "https://api.groq.com/openai/v1" || a.Config().Model.Model != "llama-3.1-8b-instant" {
		t.Fatalf("unexpected active model: %+v", a.Config().Model)
	}
	if a.Config().Model.Auth.Type != "bearer" || a.Config().Model.Auth.TokenEnv != "TEAM_GROQ_KEY" {
		t.Fatalf("unexpected auth config: %+v", a.Config().Model.Auth)
	}
	if !strings.Contains(result.text, "TEAM_GROQ_KEY") {
		t.Fatalf("expected env var guidance: %q", result.text)
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

func TestMatchFiles(t *testing.T) {
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
		matches := matchFiles(files, tc.query)
		if len(matches) != tc.want {
			t.Errorf("fuzzyFind(%q) = %d matches, want %d: %v", tc.query, len(matches), tc.want, matches)
		}
	}
}

func TestAutocompleteSelect(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	model.projectFiles = []string{"README.md", "internal/tui/tui.go", "internal/agent/agent.go"}
	model.filesLoaded = true
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
	model.filesLoaded = true
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

func TestCommandAutocomplete(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	model.input.SetValue("/he")
	model.updateAutocomplete()
	if !model.autoShow || model.autoKind != "command" {
		t.Fatalf("expected command autocomplete, got show=%v kind=%q", model.autoShow, model.autoKind)
	}
	if len(model.matches) != 1 || model.matches[0] != "/help" {
		t.Fatalf("unexpected command matches: %v", model.matches)
	}
	model.selectAutocomplete()
	if got := model.input.Value(); got != "/help " {
		t.Fatalf("selected command = %q, want /help ", got)
	}
}

func TestCommandAutocompletePreservesArguments(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	model.input.SetValue("/com existing")
	model.updateAutocomplete()
	model.selectAutocomplete()
	if got := model.input.Value(); got != "/compact existing" {
		t.Fatalf("selected command = %q, want preserved argument", got)
	}
}

func TestAutocompletePartialMatch(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	model.projectFiles = []string{"internal/tui/tui.go", "internal/tui/tui_test.go", "internal/agent/agent.go"}
	model.filesLoaded = true
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

func TestCommandPaletteShowsDescriptions(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	model.paletteOpen = true
	view := model.View()
	if !strings.Contains(view, "Command palette") || !strings.Contains(view, "Show active model and backend") {
		t.Fatalf("expected command palette descriptions: %q", view)
	}
}

func TestFuzzyPaletteFiltering(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	model.paletteQuery = "mbe"
	commands := model.filteredPalette()
	if len(commands) != 1 || commands[0].name != "/model" {
		t.Fatalf("unexpected fuzzy matches: %+v", commands)
	}
}

func TestTranscriptSearchAndJump(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	model.history = append(model.history, "You:", "inspect README", "Tool completed: read_file README.md", "Error: failed")
	model.input.SetValue("README")
	model.searchActive = true
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if len(model.searchMatches) != 2 || model.selectedHistory != model.searchMatches[0] {
		t.Fatalf("unexpected search state: matches=%v selected=%d", model.searchMatches, model.selectedHistory)
	}
	if got := model.jumpToCategory("error"); !strings.Contains(got, "Jumped") {
		t.Fatalf("expected category jump, got %q", got)
	}
}

func TestApprovalOptionsAndCountdownState(t *testing.T) {
	model := New(agent.New(agent.Options{}))
	decision := make(chan agent.ApprovalDecision, 1)
	updated, _ := model.Update(approvalPrompt{
		req:      agent.ApprovalRequest{Tool: "write_file", Kind: "write", Summary: "write_file README.md", Diff: "--- a/README.md\n+++ b/README.md"},
		decision: decision,
	})
	model = updated.(Model)
	if !strings.Contains(model.View(), "always") || !strings.Contains(model.View(), "Risk:") {
		t.Fatalf("expected rich approval state: %q", model.View())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	model = updated.(Model)
	if model.pending != nil {
		t.Fatal("expected approval to clear")
	}
	select {
	case got := <-decision:
		if got != agent.ApprovalSession {
			t.Fatalf("decision = %v, want session", got)
		}
	default:
		t.Fatal("expected approval decision")
	}
}

func TestTUIApproverTimeoutWhenNoReader(t *testing.T) {
	orig := approvalTimeout
	approvalTimeout = 10 * time.Millisecond
	defer func() { approvalTimeout = orig }()

	prompts := make(chan approvalPrompt, 1)
	app := tuiApprover{autoApprove: false, prompts: prompts}

	got := app.Approve(agent.ApprovalRequest{Kind: "write", Summary: "test"})
	if got {
		t.Fatal("expected Approve to timeout and return false")
	}
}

func TestTUIApproverAutoApprove(t *testing.T) {
	app := tuiApprover{autoApprove: true}
	if !app.Approve(agent.ApprovalRequest{Kind: "write", Summary: "test"}) {
		t.Fatal("expected auto-approve to return true")
	}
}

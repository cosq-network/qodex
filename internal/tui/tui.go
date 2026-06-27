package tui

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/benoybose/qodex/internal/agent"
	"github.com/benoybose/qodex/internal/store"
)

var approvalTimeout = 30 * time.Second

type Model struct {
	agent        *agent.Agent
	input        textarea.Model
	viewport     viewport.Model
	spinner      spinner.Model
	history      []string
	busy         bool
	lastErr      string
	width        int
	height       int
	workingIndex int
	events       chan agent.Event
	approvals    chan approvalPrompt
	pending      *approvalPrompt
	streamCh     chan string
	streamBuffer strings.Builder

	projectFiles []string
	filesLoaded  bool
	matches      []string
	matchIdx     int
	autoShow     bool
	autoQuery    string

	onQuit func()
}

type responseMsg struct {
	prompt string
	text   string
	err    error
}

type streamMsg string

type eventMsg agent.Event

type approvalPrompt struct {
	req   agent.ApprovalRequest
	reply chan bool
}

type filesLoadedMsg []string

type spinnerTickMsg spinner.TickMsg

var (
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	userStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
	aiStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	toolStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("105"))
	approvalStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	diffStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	autoStyle     = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("255"))
	autoSelStyle  = lipgloss.NewStyle().Background(lipgloss.Color("39")).Foreground(lipgloss.Color("0"))
	spinnerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
)

func New(agent *agent.Agent) Model {
	return newModel(agent, nil, false)
}

func NewAutoApproved(agent *agent.Agent) Model {
	return newModel(agent, nil, true)
}

func NewWithHistory(agent *agent.Agent, messages []store.Message) Model {
	return newModel(agent, messages, false)
}

func NewWithHistoryAutoApproved(agent *agent.Agent, messages []store.Message) Model {
	return newModel(agent, messages, true)
}

func newModel(a *agent.Agent, messages []store.Message, autoApprove bool) Model {
	ta := textarea.New()
	ta.KeyMap.InsertNewline.SetKeys("ctrl+j", "alt+enter")
	ta.Placeholder = "Ask Qodex to inspect, explain, edit, or run tests...  (@ to reference files)"
	ta.SetWidth(80)
	ta.SetHeight(4)
	ta.MaxHeight = 12
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.FocusedStyle.Prompt = lipgloss.NewStyle()
	ta.BlurredStyle.Prompt = lipgloss.NewStyle()
	ta.Focus()

	vp := viewport.New(80, 24)
	history := []string{
		headerStyle.Render("Qodex"),
		helpStyle.Render("Local coding agent. Enter submits. Ctrl+C quits. Approve tool requests with y or n."),
	}
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			history = append(history, "", userStyle.Render("You:"), msg.Content)
		case "assistant":
			history = append(history, "", aiStyle.Render("Qodex:"), msg.Content)
		case "tool":
			history = append(history, toolStyle.Render(compact(msg.Content, 500)))
		}
	}

	events := make(chan agent.Event, 500)
	approvals := make(chan approvalPrompt, 1)
	streamCh := make(chan string, 200)
	a.SetObserver(agent.ObserverFunc(func(event agent.Event) {
		select {
		case events <- event:
		default:
		}
	}))
	a.SetApprover(tuiApprover{autoApprove: autoApprove, prompts: approvals})
	a.SetStreamCallback(func(content string) {
		select {
		case streamCh <- content:
		default:
		}
	})

	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(spinnerStyle))

	return Model{
		agent:        a,
		input:        ta,
		viewport:     vp,
		spinner:      sp,
		history:      history,
		workingIndex: -1,
		events:       events,
		approvals:    approvals,
		streamCh:     streamCh,
		projectFiles: nil,
		matchIdx:     -1,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spinner.Tick, waitForEvent(m.events), waitForApproval(m.approvals), waitForStream(m.streamCh), loadProjectFiles(m.agent.ProjectRoot()))
}

func (m Model) WithQuitCallback(cb func()) Model {
	m.onQuit = cb
	return m
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(max(20, msg.Width-4))
		m.viewport.Width = msg.Width
		m.viewport.Height = max(5, msg.Height-6)
		m.refresh()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			if m.pending != nil {
				m.pending.reply <- false
			}
			if m.autoShow {
				m.clearAutocomplete()
				return m, nil
			}
			if m.onQuit != nil {
				m.onQuit()
			}
			return m, tea.Quit

		case "y", "Y":
			if m.pending != nil {
				m.pending.reply <- true
				m.history = append(m.history, approvalStyle.Render("Approved."))
				m.pending = nil
				m.refresh()
				return m, nil
			}
		case "n", "N":
			if m.pending != nil {
				m.pending.reply <- false
				m.history = append(m.history, approvalStyle.Render("Denied."))
				m.pending = nil
				m.refresh()
				return m, nil
			}

		case "tab":
			if m.autoShow && len(m.matches) > 0 {
				m.selectAutocomplete()
				return m, nil
			}

		case "up":
			if m.autoShow && len(m.matches) > 0 {
				m.matchIdx--
				if m.matchIdx < 0 {
					m.matchIdx = len(m.matches) - 1
				}
				return m, nil
			}
		case "down":
			if m.autoShow && len(m.matches) > 0 {
				m.matchIdx++
				if m.matchIdx >= len(m.matches) {
					m.matchIdx = 0
				}
				return m, nil
			}

		case "enter":
			if m.pending != nil {
				return m, nil
			}
			if m.autoShow && len(m.matches) > 0 {
				m.selectAutocomplete()
				return m, nil
			}
			if m.busy {
				return m, nil
			}
			prompt := strings.TrimSpace(m.input.Value())
			if prompt == "" {
				return m, nil
			}
			m.input.Reset()
			m.busy = true
			m.lastErr = ""
			m.streamBuffer.Reset()
			m.history = append(m.history, "", userStyle.Render("You:"), prompt, "", aiStyle.Render("Qodex:"), "")
			m.workingIndex = len(m.history) - 1
			m.refresh()
			return m, tea.Batch(
				runPrompt(m.agent, prompt),
				m.spinner.Tick,
			)
		}

	case filesLoadedMsg:
		m.projectFiles = []string(msg)
		m.filesLoaded = true
		return m, nil

	case spinner.TickMsg:
		if m.busy {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case streamMsg:
		m.streamBuffer.WriteString(string(msg))
		if m.workingIndex >= 0 && m.workingIndex < len(m.history) {
			m.history[m.workingIndex] = m.streamBuffer.String()
		}
		m.refresh()
		return m, waitForStream(m.streamCh)

	case eventMsg:
		evt := agent.Event(msg)
		if evt.Type == "tool_failed" {
			m.lastErr = evt.Error
			if m.lastErr == "" {
				m.lastErr = evt.Summary
			}
		}
		m.history = append(m.history, renderEvent(evt))
		m.refresh()
		return m, waitForEvent(m.events)

	case approvalPrompt:
		m.pending = &msg
		m.history = append(m.history, "", approvalStyle.Render("Approval required:"), renderApproval(msg.req))
		m.refresh()
		return m, waitForApproval(m.approvals)

	case responseMsg:
		m.busy = false
		finalText := msg.text
		if m.streamBuffer.Len() > 0 {
			finalText = m.streamBuffer.String()
			m.streamBuffer.Reset()
		}
		if m.workingIndex >= 0 && m.workingIndex < len(m.history) {
			m.history = append(m.history[:m.workingIndex], m.history[m.workingIndex+1:]...)
		}
		m.workingIndex = -1
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.history = append(m.history, errorStyle.Render("Error: "+msg.err.Error()))
		} else {
			m.lastErr = ""
			if finalText == "" {
				finalText = "(empty response)"
			}
			m.history = append(m.history, finalText)
		}
		m.refresh()
		return m, nil
	}

	if m.pending != nil {
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.updateAutocomplete()
	return m, cmd
}

func (m Model) View() string {
	status := ""
	if m.busy {
		status = m.spinner.View() + helpStyle.Render(" Running agent...")
	} else if m.pending != nil {
		status = approvalStyle.Render("Approval pending: y approve | n deny | Ctrl+C quit")
	} else {
		status = helpStyle.Render("Enter submit | Ctrl+C quit | @ reference files")
	}
	if m.lastErr != "" && !m.busy && m.pending == nil {
		status = errorStyle.Render("Last error: "+compact(m.lastErr, 80)) + "\n" + status
	}

	autoView := ""
	if m.autoShow && len(m.matches) > 0 {
		var b strings.Builder
		b.WriteString("\n")
		for i, match := range m.matches {
			line := "  " + match
			if i == m.matchIdx {
				line = autoSelStyle.Render("▸ " + match)
			} else {
				line = autoStyle.Render("  " + match)
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		autoView = b.String()
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.viewport.View(),
		autoView,
		status,
		m.input.View(),
	)
}

func (m *Model) refresh() {
	m.viewport.SetContent(strings.Join(m.history, "\n"))
	m.viewport.GotoBottom()
}

func (m *Model) updateAutocomplete() {
	if !m.filesLoaded || m.projectFiles == nil {
		return
	}
	val := m.input.Value()
	query := extractAutoQuery(val)
	if query == "" {
		m.clearAutocomplete()
		return
	}
	if query == m.autoQuery && m.autoShow {
		return
	}
	m.autoQuery = query
	m.matches = matchFiles(m.projectFiles, query)
	if len(m.matches) > 10 {
		m.matches = m.matches[:10]
	}
	if len(m.matches) == 0 {
		m.clearAutocomplete()
		return
	}
	m.autoShow = true
	m.matchIdx = 0
}

func (m *Model) clearAutocomplete() {
	m.autoShow = false
	m.autoQuery = ""
	m.matches = nil
	m.matchIdx = -1
}

func (m *Model) selectAutocomplete() {
	if m.matchIdx < 0 || m.matchIdx >= len(m.matches) {
		m.clearAutocomplete()
		return
	}
	selected := m.matches[m.matchIdx]
	val := m.input.Value()
	atIdx := strings.LastIndex(val, "@")
	if atIdx < 0 {
		m.clearAutocomplete()
		return
	}
	before := val[:atIdx]
	after := val[atIdx+1+len(m.autoQuery):]
	m.input.SetValue(before + "@" + selected + after)
	m.clearAutocomplete()
}

func runPrompt(a *agent.Agent, prompt string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		text, err := a.Run(ctx, prompt)
		return responseMsg{prompt: prompt, text: text, err: err}
	}
}

type tuiApprover struct {
	autoApprove bool
	prompts     chan approvalPrompt
}

func (a tuiApprover) Approve(req agent.ApprovalRequest) bool {
	if a.autoApprove {
		return true
	}
	reply := make(chan bool, 1)
	select {
	case a.prompts <- approvalPrompt{req: req, reply: reply}:
		select {
		case result := <-reply:
			return result
		case <-time.After(approvalTimeout):
			return false
		}
	case <-time.After(approvalTimeout):
		return false
	}
}

func loadProjectFiles(root string) tea.Cmd {
	return func() tea.Msg {
		return filesLoadedMsg(listProjectFiles(root))
	}
}

func waitForEvent(events <-chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		return eventMsg(<-events)
	}
}

func waitForApproval(prompts <-chan approvalPrompt) tea.Cmd {
	return func() tea.Msg {
		return <-prompts
	}
}

func waitForStream(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		return streamMsg(<-ch)
	}
}

func renderEvent(event agent.Event) string {
	switch event.Type {
	case "context_compacted":
		return helpStyle.Render(compact(event.Summary, 80))
	case "tool_requested":
		text := toolStyle.Render(fmt.Sprintf("Tool requested [%s]: %s", event.Effect, compact(event.Summary, 500)))
		if event.Detail != "" {
			text += "\n" + diffStyle.Render(compact(event.Detail, 1000))
		}
		return text
	case "approval_requested":
		text := approvalStyle.Render(fmt.Sprintf("Approval requested [%s]", event.Effect))
		if event.Detail != "" {
			text += "\n" + diffStyle.Render(compact(event.Detail, 1000))
		}
		return text
	case "approval_approved":
		return approvalStyle.Render(fmt.Sprintf("Approval granted [%s]", event.Effect))
	case "approval_denied":
		return approvalStyle.Render(fmt.Sprintf("Approval denied [%s]", event.Effect))
	case "tool_completed":
		return toolStyle.Render(fmt.Sprintf("Tool completed: %s", compact(event.Summary, 500)))
	case "tool_failed":
		if event.Error != "" {
			return errorStyle.Render(fmt.Sprintf("Tool failed: %s", compact(event.Error, 500)))
		}
		return errorStyle.Render(fmt.Sprintf("Tool failed: %s", compact(event.Summary, 500)))
	default:
		return toolStyle.Render(compact(event.Summary, 500))
	}
}

func renderApproval(req agent.ApprovalRequest) string {
	text := fmt.Sprintf("%s\n%s", approvalStyle.Render(req.Kind), compact(req.Summary, 4000))
	if req.Diff != "" {
		text += "\n" + diffStyle.Render(compact(req.Diff, 2000))
	}
	text += "\n" + helpStyle.Render("Press y to approve, n to deny.")
	return text
}

func compact(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "\n... truncated ..."
}

func extractAutoQuery(s string) string {
	atIdx := strings.LastIndex(s, "@")
	if atIdx < 0 {
		return ""
	}
	after := s[atIdx+1:]
	end := strings.IndexAny(after, " \t\n")
	if end == 0 {
		return ""
	}
	if end < 0 {
		return strings.TrimSpace(after)
	}
	return strings.TrimSpace(after[:end])
}

func pathDepth(p string) int {
	return strings.Count(filepath.ToSlash(p), "/")
}

func matchFiles(files []string, query string) []string {
	if query == "" {
		return nil
	}
	q := strings.ToLower(query)
	var matches []string
	seen := map[string]bool{}
	for _, f := range files {
		lower := strings.ToLower(f)
		if strings.Contains(lower, q) {
			if !seen[f] {
				seen[f] = true
				matches = append(matches, f)
			}
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		return pathDepth(matches[i]) < pathDepth(matches[j])
	})
	return matches
}

func listProjectFiles(root string) []string {
	var files []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(files) >= 2000 {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "vendor" || d.Name() == ".qodex") {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			files = append(files, rel)
		}
		return nil
	})
	return files
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

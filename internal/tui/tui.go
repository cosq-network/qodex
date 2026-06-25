package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/benoybose/locha/internal/agent"
	"github.com/benoybose/locha/internal/store"
)

type Model struct {
	agent        *agent.Agent
	input        textinput.Model
	viewport     viewport.Model
	history      []string
	busy         bool
	err          error
	lastErr      string
	width        int
	height       int
	workingIndex int
	events       chan agent.Event
	approvals    chan approvalPrompt
	pending      *approvalPrompt
	streamCh     chan string
	streamBuffer strings.Builder
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

var (
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	userStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
	aiStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	toolStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("105"))
	approvalStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	diffStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
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
	input := textinput.New()
	input.Placeholder = "Ask Locha to inspect, explain, edit, or run tests..."
	input.Focus()
	input.CharLimit = 4000
	input.Prompt = "> "

	vp := viewport.New(80, 20)
	history := []string{
		headerStyle.Render("Locha"),
		helpStyle.Render("Local coding agent. Enter submits. Ctrl+C quits. Approve tool requests with y or n."),
	}
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			history = append(history, "", userStyle.Render("You:"), msg.Content)
		case "assistant":
			history = append(history, "", aiStyle.Render("Locha:"), msg.Content)
		}
	}
	events := make(chan agent.Event, 100)
	approvals := make(chan approvalPrompt)
	streamCh := make(chan string, 50)
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
	return Model{
		agent:        a,
		input:        input,
		viewport:     vp,
		history:      history,
		workingIndex: -1,
		events:       events,
		approvals:    approvals,
		streamCh:     streamCh,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, waitForEvent(m.events), waitForApproval(m.approvals), waitForStream(m.streamCh))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = max(20, msg.Width-4)
		m.viewport.Width = msg.Width
		m.viewport.Height = max(5, msg.Height-4)
		m.refresh()
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			if m.pending != nil {
				m.pending.reply <- false
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
		case "enter":
			if m.busy || m.pending != nil {
				return m, nil
			}
			prompt := strings.TrimSpace(m.input.Value())
			if prompt == "" {
				return m, nil
			}
			m.input.SetValue("")
			m.busy = true
			m.lastErr = ""
			m.streamBuffer.Reset()
			m.history = append(m.history, "", userStyle.Render("You:"), prompt, "", aiStyle.Render("Locha:"), "Working...")
			m.workingIndex = len(m.history) - 1
			m.refresh()
			return m, runPrompt(m.agent, prompt)
		}
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
		} else if finalText != "" {
			m.lastErr = ""
			m.history = append(m.history, finalText)
		}
		m.refresh()
		return m, nil
	}

	var cmd tea.Cmd
	if m.pending != nil {
		return m, nil
	}
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	status := helpStyle.Render("Enter submit | Ctrl+C quit")
	if m.pending != nil {
		status = approvalStyle.Render("Approval pending: y approve | n deny | Ctrl+C quit")
	} else if m.busy {
		status = helpStyle.Render("Running agent...")
	}
	if m.lastErr != "" && !m.busy && m.pending == nil {
		status = errorStyle.Render("Last error: "+compact(m.lastErr, 80)) + "\n" + status
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.viewport.View(),
		status,
		m.input.View(),
	)
}

func (m *Model) refresh() {
	m.viewport.SetContent(strings.Join(m.history, "\n"))
	m.viewport.GotoBottom()
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
	reply := make(chan bool)
	a.prompts <- approvalPrompt{req: req, reply: reply}
	return <-reply
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

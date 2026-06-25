package tui

import (
	"context"
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
	agent    *agent.Agent
	input    textinput.Model
	viewport viewport.Model
	history  []string
	busy     bool
	err      error
	width    int
	height   int
}

type responseMsg struct {
	prompt string
	text   string
	err    error
}

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	userStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
	aiStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

func New(agent *agent.Agent) Model {
	return newModel(agent, nil)
}

func NewWithHistory(agent *agent.Agent, messages []store.Message) Model {
	return newModel(agent, messages)
}

func newModel(agent *agent.Agent, messages []store.Message) Model {
	input := textinput.New()
	input.Placeholder = "Ask Locha to inspect, explain, edit, or run tests..."
	input.Focus()
	input.CharLimit = 4000
	input.Prompt = "> "

	vp := viewport.New(80, 20)
	history := []string{
		headerStyle.Render("Locha"),
		helpStyle.Render("Local coding agent. Enter submits. Ctrl+C quits. Use --yes for write/shell approval in this MVP."),
	}
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			history = append(history, "", userStyle.Render("You:"), msg.Content)
		case "assistant":
			history = append(history, "", aiStyle.Render("Locha:"), msg.Content)
		}
	}
	return Model{
		agent:    agent,
		input:    input,
		viewport: vp,
		history:  history,
	}
}

func (m Model) Init() tea.Cmd {
	return textinput.Blink
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
			return m, tea.Quit
		case "enter":
			if m.busy {
				return m, nil
			}
			prompt := strings.TrimSpace(m.input.Value())
			if prompt == "" {
				return m, nil
			}
			m.input.SetValue("")
			m.busy = true
			m.history = append(m.history, "", userStyle.Render("You:"), prompt, "", aiStyle.Render("Locha:"), "Working...")
			m.refresh()
			return m, runPrompt(m.agent, prompt)
		}
	case responseMsg:
		m.busy = false
		if len(m.history) > 0 && m.history[len(m.history)-1] == "Working..." {
			m.history = m.history[:len(m.history)-1]
		}
		if msg.err != nil {
			m.history = append(m.history, errorStyle.Render(msg.err.Error()))
		} else {
			m.history = append(m.history, msg.text)
		}
		m.refresh()
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	status := helpStyle.Render("Enter submit | Ctrl+C quit")
	if m.busy {
		status = helpStyle.Render("Running agent...")
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

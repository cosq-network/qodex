package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/benoybose/qodex/internal/agent"
	"github.com/benoybose/qodex/internal/mcp"
	"github.com/benoybose/qodex/internal/skills"
	"github.com/benoybose/qodex/internal/store"
)

var approvalTimeout = 30 * time.Second

type Model struct {
	agent            *agent.Agent
	input            textarea.Model
	viewport         viewport.Model
	spinner          spinner.Model
	history          []string
	busy             bool
	busyLabel        string
	currentTool      string
	startedAt        time.Time
	lastErr          string
	width            int
	height           int
	workingIndex     int
	events           chan agent.Event
	approvals        chan approvalPrompt
	pending          *approvalPrompt
	approvalDeadline time.Time
	approvalExpanded bool
	streamCh         chan string
	streamBuffer     strings.Builder
	lastResponse     string
	collapseDetails  bool
	selectedHistory  int
	searchActive     bool
	searchQuery      string
	searchMatches    []int
	searchIndex      int
	paletteOpen      bool
	paletteQuery     string
	paletteIndex     int

	projectFiles []string
	filesLoaded  bool
	matches      []string
	matchIdx     int
	autoShow     bool
	autoQuery    string
	autoKind     string

	onQuit    func()
	runCancel context.CancelFunc
}

type responseMsg struct {
	prompt string
	text   string
	err    error
}

type slashResultMsg struct {
	text string
	err  error
}

type streamMsg string

type eventMsg agent.Event

type approvalPrompt struct {
	req      agent.ApprovalRequest
	reply    chan bool
	decision chan agent.ApprovalDecision
}

type approvalTickMsg time.Time

type filesLoadedMsg []string

type spinnerTickMsg spinner.TickMsg

var (
	headerStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	userStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("220"))
	aiStyle         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	toolStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("105"))
	approvalStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	errorStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	helpStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	diffStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	autoStyle       = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("255"))
	autoSelStyle    = lipgloss.NewStyle().Background(lipgloss.Color("39")).Foreground(lipgloss.Color("0"))
	spinnerStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	inputFrameStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Padding(0, 1)
	statusBarStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	paletteStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("39")).Padding(0, 1)
)

const composerPlaceholder = "Ask Qodex to inspect, explain, edit, or run tests...  (@ to reference files)"

var ansiRegexp = regexp.MustCompile("\\x1b\\[[0-9;]*[A-Za-z]")

type paletteCommand struct {
	name        string
	description string
	shortcut    string
}

var paletteCommands = []paletteCommand{
	{name: "/help", description: "Show command help", shortcut: "?"},
	{name: "/model", description: "Show active model and backend", shortcut: ""},
	{name: "/session", description: "Show current session details", shortcut: ""},
	{name: "/clear", description: "Clear the visible transcript", shortcut: ""},
	{name: "/export", description: "Export the current session as JSON", shortcut: ""},
	{name: "/theme", description: "Show the active terminal theme", shortcut: ""},
	{name: "/settings", description: "Show effective runtime settings", shortcut: ""},
	{name: "/jump", description: "Jump to user, tool, approval, or error", shortcut: ""},
	{name: "/skills", description: "List discovered skills", shortcut: ""},
	{name: "/plan", description: "Show task progress and plan state", shortcut: ""},
	{name: "/compact", description: "Compact the active conversation", shortcut: ""},
	{name: "/mcp", description: "Diagnose configured MCP servers", shortcut: ""},
	{name: "/commit", description: "Create an approval-aware Git commit", shortcut: ""},
	{name: "/undo", description: "Revert a commit through the agent", shortcut: ""},
}

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
	ta.Placeholder = composerPlaceholder
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
		helpStyle.Render("Local coding agent  •  Enter to submit  •  Ctrl+C to quit  •  y/n for approvals"),
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
	a.SetApprover(&tuiApprover{autoApprove: autoApprove, prompts: approvals, approvedTools: map[string]agent.ApprovalDecision{}})
	a.SetStreamCallback(func(content string) {
		select {
		case streamCh <- content:
		default:
		}
	})

	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(spinnerStyle))

	return Model{
		agent:           a,
		input:           ta,
		viewport:        vp,
		spinner:         sp,
		history:         history,
		workingIndex:    -1,
		selectedHistory: -1,
		paletteIndex:    0,
		events:          events,
		approvals:       approvals,
		streamCh:        streamCh,
		projectFiles:    nil,
		matchIdx:        -1,
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
		m.viewport.Width = max(20, msg.Width)
		m.viewport.Height = max(5, msg.Height-m.input.Height()-4)
		m.refresh()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+p":
			m.paletteOpen = !m.paletteOpen
			m.paletteIndex = 0
			m.input.Reset()
			if m.paletteOpen {
				m.input.Placeholder = "Filter commands..."
			} else {
				m.input.Placeholder = composerPlaceholder
			}
			return m, nil
		case "ctrl+f":
			m.searchActive = true
			m.input.Reset()
			m.input.Placeholder = "Search transcript..."
			m.clearAutocomplete()
			return m, nil
		case "ctrl+n":
			if len(m.searchMatches) > 0 {
				m.searchIndex = (m.searchIndex + 1) % len(m.searchMatches)
				m.selectedHistory = m.searchMatches[m.searchIndex]
				m.viewport.SetYOffset(m.searchMatches[m.searchIndex])
				return m, nil
			}
		case "ctrl+y":
			if err := m.copySelected(); err != nil {
				m.lastErr = err.Error()
			} else {
				m.lastErr = ""
			}
			return m, nil
		case "ctrl+o":
			if m.pending != nil {
				m.approvalExpanded = !m.approvalExpanded
				if len(m.history) > 0 {
					m.history[len(m.history)-1] = renderApprovalWithOptions(m.pending.req, m.approvalExpanded)
				}
				m.refresh()
				return m, nil
			}
			m.collapseDetails = !m.collapseDetails
			m.history = append(m.history, helpStyle.Render(fmt.Sprintf("Tool details %s.", map[bool]string{true: "collapsed", false: "expanded"}[m.collapseDetails])))
			m.refresh()
			return m, nil
		case "1", "2", "3":
			if m.pending != nil {
				decision := agent.ApprovalOnce
				if msg.String() == "2" {
					decision = agent.ApprovalSession
				} else if msg.String() == "3" {
					decision = agent.ApprovalAlways
				}
				m.respondApproval(decision)
				return m, nil
			}
		case "left", "right":
			if m.paletteOpen {
				return m, nil
			}
		case "ctrl+c":
			if m.pending != nil {
				m.respondApproval(agent.ApprovalDeny)
			}
			if m.runCancel != nil {
				m.runCancel()
				m.runCancel = nil
			}
			if m.autoShow {
				m.clearAutocomplete()
				return m, nil
			}
			if m.onQuit != nil {
				m.onQuit()
			}
			return m, tea.Quit

		case "esc":
			if m.paletteOpen {
				m.paletteOpen = false
				m.input.Reset()
				m.input.Placeholder = composerPlaceholder
				return m, nil
			}
			if m.searchActive {
				m.searchActive = false
				m.input.Placeholder = composerPlaceholder
				m.input.Reset()
				return m, nil
			}
			if m.pending != nil {
				m.respondApproval(agent.ApprovalDeny)
				m.history = append(m.history, approvalStyle.Render("Approval cancelled."))
				m.refresh()
				return m, nil
			}
			if m.autoShow {
				m.clearAutocomplete()
				return m, nil
			}
			if m.runCancel != nil {
				m.runCancel()
				m.runCancel = nil
				return m, nil
			}
			return m, nil

		case "y", "Y":
			if m.pending != nil {
				m.respondApproval(agent.ApprovalOnce)
				m.history = append(m.history, approvalStyle.Render("Approved."))
				m.refresh()
				return m, nil
			}
		case "n", "N":
			if m.pending != nil {
				m.respondApproval(agent.ApprovalDeny)
				m.history = append(m.history, approvalStyle.Render("Denied."))
				m.refresh()
				return m, nil
			}

		case "tab":
			if m.autoShow && len(m.matches) > 0 {
				m.selectAutocomplete()
				return m, nil
			}

		case "up":
			if m.paletteOpen {
				m.paletteIndex--
				if m.paletteIndex < 0 {
					m.paletteIndex = len(m.filteredPalette()) - 1
				}
				return m, nil
			}
			if m.autoShow && len(m.matches) > 0 {
				m.matchIdx--
				if m.matchIdx < 0 {
					m.matchIdx = len(m.matches) - 1
				}
				return m, nil
			}
		case "down":
			if m.paletteOpen {
				m.paletteIndex++
				if m.paletteIndex >= len(m.filteredPalette()) {
					m.paletteIndex = 0
				}
				return m, nil
			}
			if m.autoShow && len(m.matches) > 0 {
				m.matchIdx++
				if m.matchIdx >= len(m.matches) {
					m.matchIdx = 0
				}
				return m, nil
			}

		case "enter":
			if m.paletteOpen {
				return m, m.executePaletteCommand()
			}
			if m.searchActive {
				m.searchActive = false
				m.input.Placeholder = composerPlaceholder
				m.searchQuery = strings.TrimSpace(m.input.Value())
				m.searchMatches = m.findTranscript(m.searchQuery)
				m.searchIndex = 0
				if len(m.searchMatches) > 0 {
					m.selectedHistory = m.searchMatches[0]
					m.viewport.SetYOffset(m.searchMatches[0])
					m.lastErr = ""
				} else {
					m.lastErr = "No transcript matches for " + strconv.Quote(m.searchQuery)
				}
				m.input.Reset()
				return m, nil
			}
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
			if m.runCancel != nil {
				m.runCancel()
				m.runCancel = nil
			}
			prompt := strings.TrimSpace(m.input.Value())
			if prompt == "" {
				return m, nil
			}
			m.input.Reset()
			displayPrompt := prompt
			if handled, immediate, actionPrompt := m.handleSlashCommand(prompt); handled {
				m.history = append(m.history, "", userStyle.Render("You:"), displayPrompt)
				if immediate != "" {
					m.history = append(m.history, immediate)
					m.refresh()
					return m, nil
				}
				if actionPrompt == "" {
					m.refresh()
					return m, nil
				}
				if strings.HasPrefix(actionPrompt, "__slash_skills:") {
					m.refresh()
					return m, runSlashSkills(m.agent, strings.TrimPrefix(actionPrompt, "__slash_skills:"))
				}
				if strings.HasPrefix(actionPrompt, "__slash_mcp:") {
					m.refresh()
					return m, runSlashMCP(m.agent, strings.TrimPrefix(actionPrompt, "__slash_mcp:"))
				}
				if actionPrompt == "__slash_export" {
					m.refresh()
					return m, runSlashExport(m.agent)
				}
				prompt = actionPrompt
			}
			m.busy = true
			m.busyLabel = "Running agent"
			m.startedAt = time.Now()
			m.lastErr = ""
			m.streamBuffer.Reset()
			m.history = append(m.history, "", userStyle.Render("You:"), prompt, "", aiStyle.Render("Qodex:"), "")
			m.workingIndex = len(m.history) - 1
			m.refresh()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			m.runCancel = cancel
			return m, tea.Batch(
				runPrompt(m.agent, prompt, ctx),
				m.spinner.Tick,
			)
		}

	case filesLoadedMsg:
		m.projectFiles = []string(msg)
		m.filesLoaded = true
		return m, nil

	case approvalTickMsg:
		if m.pending != nil && !m.approvalDeadline.IsZero() && time.Now().Before(m.approvalDeadline) {
			return m, approvalTick()
		}
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
		if evt.Tool != "" {
			m.currentTool = evt.Tool
		}
		if evt.Type == "tool_completed" || evt.Type == "tool_failed" {
			m.currentTool = ""
		}
		if evt.Type == "tool_failed" {
			m.lastErr = evt.Error
			if m.lastErr == "" {
				m.lastErr = evt.Summary
			}
		}
		m.history = append(m.history, renderEventWithDetails(evt, m.collapseDetails))
		m.selectedHistory = len(m.history) - 1
		m.refresh()
		return m, waitForEvent(m.events)

	case approvalPrompt:
		m.pending = &msg
		m.approvalDeadline = time.Now().Add(approvalTimeout)
		m.approvalExpanded = false
		m.history = append(m.history, "", approvalStyle.Render("Approval required:"), renderApproval(msg.req))
		m.refresh()
		return m, tea.Batch(waitForApproval(m.approvals), approvalTick())

	case responseMsg:
		m.busy = false
		m.busyLabel = ""
		if m.runCancel != nil {
			m.runCancel()
			m.runCancel = nil
		}
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
			m.lastResponse = finalText
			m.selectedHistory = len(m.history) - 1
		}
		m.refresh()
		return m, nil

	case slashResultMsg:
		m.busy = false
		m.busyLabel = ""
		if msg.err != nil {
			m.history = append(m.history, errorStyle.Render("Error: "+msg.err.Error()))
		} else {
			m.history = append(m.history, msg.text)
			m.selectedHistory = len(m.history) - 1
		}
		m.refresh()
		return m, nil
	}

	if m.pending != nil {
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.paletteOpen {
		m.paletteQuery = m.input.Value()
		filtered := m.filteredPalette()
		if len(filtered) == 0 {
			m.paletteIndex = 0
		} else if m.paletteIndex >= len(filtered) {
			m.paletteIndex = len(filtered) - 1
		}
		return m, cmd
	}
	m.updateAutocomplete()
	return m, cmd
}

func (m *Model) handleSlashCommand(input string) (handled bool, immediate string, actionPrompt string) {
	parts := strings.Fields(input)
	if len(parts) == 0 || !strings.HasPrefix(parts[0], "/") {
		return false, "", ""
	}
	command := strings.ToLower(parts[0])
	args := strings.TrimSpace(strings.TrimPrefix(input, parts[0]))
	switch command {
	case "/skill":
		// Preserve the existing explicit skill-routing prompt behavior.
		return false, "", ""
	case "/help":
		return true, slashHelp(), ""
	case "/model", "/session", "/theme", "/settings":
		return true, helpStyle.Render(m.localCommandOutput(command)), ""
	case "/clear":
		m.clearTranscript()
		return true, helpStyle.Render("Transcript cleared."), ""
	case "/export":
		m.busy = true
		m.busyLabel = "Exporting session"
		return true, "", "__slash_export"
	case "/jump":
		return true, m.jumpToCategory(args), ""
	case "/skills":
		m.busy = true
		m.busyLabel = "Discovering skills"
		return true, "", "__slash_skills:" + args
	case "/plan":
		return true, m.agent.PlanSummary(), ""
	case "/compact":
		m.agent.CompactContext()
		return true, helpStyle.Render("Conversation context compacted."), ""
	case "/mcp":
		m.busy = true
		m.busyLabel = "Diagnosing MCP servers"
		return true, "", "__slash_mcp:" + args
	case "/commit":
		if args == "" {
			args = "Create a focused commit from the current intended changes with a detailed commit message."
		} else {
			args = fmt.Sprintf("Create a focused Git commit with this commit message: %q", args)
		}
		return true, "", args
	case "/undo":
		if args == "" {
			args = "Use git_undo to revert the latest commit, preserving history."
		} else {
			args = fmt.Sprintf("Use git_undo to revert commit %q, preserving history.", args)
		}
		return true, "", args
	default:
		return true, errorStyle.Render("Unknown slash command. Type /help for available commands."), ""
	}
}

func (m *Model) respondApproval(decision agent.ApprovalDecision) {
	if m.pending == nil {
		return
	}
	if m.pending.decision != nil {
		m.pending.decision <- decision
	} else if m.pending.reply != nil {
		m.pending.reply <- decision != agent.ApprovalDeny
	}
	m.pending = nil
	m.approvalDeadline = time.Time{}
	m.approvalExpanded = false
}

func approvalTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return approvalTickMsg(t) })
}

func (m Model) filteredPalette() []paletteCommand {
	query := strings.ToLower(strings.TrimSpace(m.paletteQuery))
	if query == "" {
		return paletteCommands
	}
	var out []paletteCommand
	for _, command := range paletteCommands {
		if fuzzyMatch(query, command.name+" "+command.description) {
			out = append(out, command)
		}
	}
	return out
}

func (m *Model) jumpToCategory(category string) string {
	category = strings.ToLower(strings.TrimSpace(category))
	needles := map[string][]string{
		"user":     {"You:"},
		"tool":     {"Tool requested", "Tool completed", "Tool failed"},
		"approval": {"Approval required", "Approval requested", "Approval granted", "Approval denied"},
		"error":    {"Error:", "Tool failed"},
	}
	for i := len(m.history) - 1; i >= 0; i-- {
		plain := stripANSI(m.history[i])
		for _, needle := range needles[category] {
			if strings.Contains(plain, needle) {
				m.selectedHistory = i
				m.viewport.SetYOffset(i)
				return fmt.Sprintf("Jumped to %s at transcript line %d.", category, i+1)
			}
		}
	}
	return fmt.Sprintf("No %s entries found. Use /jump user|tool|approval|error.", category)
}

func fuzzyMatch(query, value string) bool {
	query = strings.ToLower(query)
	value = strings.ToLower(value)
	pos := 0
	for _, r := range query {
		i := strings.IndexRune(value[pos:], r)
		if i < 0 {
			return false
		}
		pos += i + 1
	}
	return true
}

func (m *Model) executePaletteCommand() tea.Cmd {
	commands := m.filteredPalette()
	if len(commands) == 0 || m.paletteIndex >= len(commands) {
		m.paletteOpen = false
		return nil
	}
	command := commands[m.paletteIndex].name
	m.paletteOpen = false
	m.paletteQuery = ""
	m.input.Reset()
	m.input.Placeholder = composerPlaceholder
	if command == "/clear" {
		m.clearTranscript()
		return nil
	}
	if command == "/export" {
		m.busy = true
		m.busyLabel = "Exporting session"
		return runSlashExport(m.agent)
	}
	if command == "/model" || command == "/session" || command == "/theme" || command == "/settings" {
		m.history = append(m.history, "", helpStyle.Render(m.localCommandOutput(command)))
		m.refresh()
		return nil
	}
	if handled, immediate, action := m.handleSlashCommand(command); handled {
		if immediate != "" {
			m.history = append(m.history, "", immediate)
			m.refresh()
			return nil
		}
		if strings.HasPrefix(action, "__slash_skills:") {
			return runSlashSkills(m.agent, strings.TrimPrefix(action, "__slash_skills:"))
		}
		if strings.HasPrefix(action, "__slash_mcp:") {
			return runSlashMCP(m.agent, strings.TrimPrefix(action, "__slash_mcp:"))
		}
	}
	return nil
}

func (m *Model) clearTranscript() {
	m.history = []string{headerStyle.Render("Qodex"), helpStyle.Render("Transcript cleared. Use Ctrl+F to search new output.")}
	m.workingIndex = -1
	m.selectedHistory = -1
	m.refresh()
}

func (m Model) localCommandOutput(command string) string {
	cfg := m.agent.Config()
	switch command {
	case "/model":
		return fmt.Sprintf("Model: %s\nBackend: %s", cfg.Model.Model, cfg.Runtime.Backend)
	case "/session":
		return fmt.Sprintf("Session: %d\nProject: %s", m.agent.SessionID(), cfg.ProjectRoot)
	case "/theme":
		return "Theme: Qodex default (terminal colors)"
	case "/settings":
		return fmt.Sprintf("Context limit: %d tokens\nMax agent steps: %d\nTool calling: %s", cfg.Runtime.ContextTokens, cfg.Agent.MaxSteps, cfg.Agent.ToolCalls)
	default:
		return ""
	}
}

func (m Model) findTranscript(query string) []int {
	if query == "" {
		return nil
	}
	query = strings.ToLower(query)
	var matches []int
	for i, line := range m.history {
		if strings.Contains(strings.ToLower(stripANSI(line)), query) {
			matches = append(matches, i)
		}
	}
	return matches
}

func (m *Model) copySelected() error {
	text := m.lastResponse
	if m.selectedHistory >= 0 && m.selectedHistory < len(m.history) {
		text = stripANSI(m.history[m.selectedHistory])
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("nothing to copy")
	}
	return clipboard.WriteAll(text)
}

func stripANSI(value string) string { return ansiRegexp.ReplaceAllString(value, "") }

func runSlashExport(a *agent.Agent) tea.Cmd {
	return func() tea.Msg {
		data, err := a.ExportSession(context.Background())
		if err != nil {
			return slashResultMsg{err: err}
		}
		encoded, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return slashResultMsg{err: err}
		}
		if err := clipboard.WriteAll(string(encoded)); err != nil {
			return slashResultMsg{err: fmt.Errorf("copy session export: %w", err)}
		}
		return slashResultMsg{text: fmt.Sprintf("Session %d exported as JSON to the clipboard (%d bytes).", data.Session.ID, len(encoded))}
	}
}

func slashHelp() string {
	return helpStyle.Render(strings.TrimSpace(`Slash commands:

/help              Show this help
/model             Show active model and backend
/session           Show current session details
/clear             Clear the visible transcript
/export            Copy the current session as JSON
/theme             Show the active terminal theme
/settings          Show effective runtime settings
/jump [CATEGORY]   Jump to user/tool/approval/error entries
/skills            List discovered skills
/plan              Show the current task and recorded actions
/compact           Compact the conversation context
/mcp [NAME]        Diagnose configured MCP servers
/commit [MESSAGE]  Create a focused Git commit through the agent
/undo [COMMIT]     Revert a commit through the agent

Use /skill <name> in a prompt to explicitly route a skill.`))
}

func runSlashSkills(a *agent.Agent, filter string) tea.Cmd {
	return func() tea.Msg {
		found, err := skills.Discover(a.ProjectRoot())
		if err != nil {
			return slashResultMsg{err: err}
		}
		filter = strings.TrimSpace(strings.ToLower(filter))
		var b strings.Builder
		b.WriteString("Discovered skills:\n")
		count := 0
		for _, skill := range found {
			if filter != "" && !strings.Contains(strings.ToLower(skill.Name), filter) {
				continue
			}
			fmt.Fprintf(&b, "- %s (%s)\n", skill.Name, skill.Path)
			count++
		}
		if count == 0 {
			b.WriteString("No matching skills found.\n")
		}
		return slashResultMsg{text: b.String()}
	}
}

func runSlashMCP(a *agent.Agent, only string) tea.Cmd {
	return func() tea.Msg {
		cfg := a.Config()
		names := make([]string, 0, len(cfg.MCP.Servers))
		for name := range cfg.MCP.Servers {
			if only == "" || name == only {
				names = append(names, name)
			}
		}
		if only != "" && len(names) == 0 {
			return slashResultMsg{err: fmt.Errorf("MCP server %q is not configured", only)}
		}
		sort.Strings(names)
		var b strings.Builder
		for _, name := range names {
			server := cfg.MCP.Servers[name]
			if !server.Enabled {
				fmt.Fprintf(&b, "MCP %s: disabled\n", name)
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			diag := mcp.Diagnose(ctx, mcp.ServerConfig{
				Transport: server.Transport,
				Command:   server.Command,
				Args:      server.Args,
				Endpoint:  server.URL,
				Headers:   server.Headers,
				Env:       server.Env,
				Auth:      mcp.AuthConfig{Type: server.Auth.Type, TokenEnv: server.Auth.TokenEnv, PassEnv: server.Auth.PassEnv, Header: server.Auth.Header},
			})
			cancel()
			if !diag.Healthy {
				fmt.Fprintf(&b, "MCP %s: failed\n  error: %s\n  hint: %s\n", name, diag.Error, diag.Hint)
				continue
			}
			endpoint := diag.ResolvedCommand
			if diag.Transport == "streamable-http" {
				endpoint = diag.Endpoint
			}
			fmt.Fprintf(&b, "MCP %s: ok (transport=%s endpoint=%s protocol=%s tools=%d)\n", name, diag.Transport, endpoint, diag.Protocol, diag.ToolCount)
		}
		if len(names) == 0 {
			b.WriteString("No MCP servers configured.\n")
		}
		return slashResultMsg{text: b.String()}
	}
}

func (m Model) View() string {
	status := ""
	if m.busy {
		label := m.busyLabel
		if label == "" {
			label = "Running agent"
		}
		status = m.spinner.View() + helpStyle.Render(" "+label+"…  (Esc cancels)")
		if m.currentTool != "" {
			status += toolStyle.Render("  •  " + m.currentTool)
		}
	} else if m.pending != nil {
		remaining := int(time.Until(m.approvalDeadline).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		status = approvalStyle.Render(fmt.Sprintf("Approval pending  •  1 once  •  2 session  •  3 always  •  n deny  •  %ds", remaining))
	} else {
		status = statusBarStyle.Render("Enter submit  •  Ctrl+P palette  •  Ctrl+F search  •  Ctrl+Y copy  •  Ctrl+C quit")
	}
	if m.lastErr != "" && !m.busy && m.pending == nil {
		status = errorStyle.Render("Last error: "+compact(m.lastErr, 80)) + "\n" + status
	}
	progress := m.progressView()

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
	paletteView := ""
	if m.paletteOpen {
		var b strings.Builder
		b.WriteString(headerStyle.Render("Command palette"))
		b.WriteString("\n")
		commands := m.filteredPalette()
		for i, command := range commands {
			line := fmt.Sprintf("  %-12s %s", command.name, command.description)
			if command.shortcut != "" {
				line += "  [" + command.shortcut + "]"
			}
			if i == m.paletteIndex {
				line = autoSelStyle.Render("▸ " + line)
			} else {
				line = autoStyle.Render(line)
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		paletteView = paletteStyle.Render(strings.TrimRight(b.String(), "\n"))
	}

	inputWidth := max(20, m.width-2)
	if m.width == 0 {
		inputWidth = 78
	}
	inputView := inputFrameStyle.Width(inputWidth).Render(m.input.View())
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.viewport.View(),
		progress,
		autoView,
		paletteView,
		status,
		inputView,
	)
}

func (m Model) progressView() string {
	if !m.busy && m.pending == nil {
		return ""
	}
	p := m.agent.Progress()
	parts := []string{}
	if p.CurrentTask != "" {
		parts = append(parts, "Task: "+compact(p.CurrentTask, 60))
	}
	if len(p.ActiveSkills) > 0 {
		parts = append(parts, "Skills: "+strings.Join(p.ActiveSkills, ", "))
	}
	if len(p.FilesInspected) > 0 {
		parts = append(parts, fmt.Sprintf("Files: %d", len(p.FilesInspected)))
	}
	if p.CurrentStep > 0 {
		parts = append(parts, fmt.Sprintf("Step: %d/%d", p.CurrentStep, p.MaxSteps))
	}
	if p.ContextLimit > 0 {
		parts = append(parts, fmt.Sprintf("Context: %d/%d", p.ContextTokens, p.ContextLimit))
		if p.ContextTokens >= int(float64(p.ContextLimit)*0.7) {
			parts = append(parts, "Compaction soon")
		}
	}
	if m.startedAt.IsZero() == false {
		parts = append(parts, "Elapsed: "+time.Since(m.startedAt).Round(time.Second).String())
	}
	if len(parts) == 0 {
		return ""
	}
	return helpStyle.Render("  " + strings.Join(parts, "  •  "))
}

func (m *Model) refresh() {
	m.viewport.SetContent(strings.Join(m.history, "\n"))
	m.viewport.GotoBottom()
}

func (m *Model) updateAutocomplete() {
	val := m.input.Value()
	trimmed := strings.TrimSpace(val)
	commandToken := ""
	if fields := strings.Fields(trimmed); len(fields) > 0 {
		commandToken = fields[0]
	}
	if strings.HasPrefix(commandToken, "/") {
		query := strings.ToLower(strings.TrimPrefix(commandToken, "/"))
		m.autoQuery = query
		m.matches = matchCommands(query)
		m.autoKind = "command"
		m.autoShow = len(m.matches) > 0
		m.matchIdx = 0
		if !m.autoShow {
			m.clearAutocomplete()
		}
		return
	}
	if !m.filesLoaded || m.projectFiles == nil {
		m.clearAutocomplete()
		return
	}
	query := extractAutoQuery(val)
	if query == "" {
		m.clearAutocomplete()
		return
	}
	if query == m.autoQuery && m.autoShow {
		return
	}
	m.autoQuery = query
	m.autoKind = "file"
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

func matchCommands(query string) []string {
	commands := []string{"/help", "/skills", "/plan", "/compact", "/mcp", "/commit", "/undo", "/skill"}
	if query == "" {
		return commands
	}
	var matches []string
	for _, command := range commands {
		if strings.HasPrefix(strings.TrimPrefix(command, "/"), query) {
			matches = append(matches, command)
		}
	}
	return matches
}

func (m *Model) clearAutocomplete() {
	m.autoShow = false
	m.autoQuery = ""
	m.autoKind = ""
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
	if m.autoKind == "command" {
		if end := strings.IndexAny(val, " \t\n"); end >= 0 {
			m.input.SetValue(selected + val[end:])
		} else {
			m.input.SetValue(selected + " ")
		}
		m.clearAutocomplete()
		return
	}
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

func runPrompt(a *agent.Agent, prompt string, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		text, err := a.Run(ctx, prompt)
		return responseMsg{prompt: prompt, text: text, err: err}
	}
}

type tuiApprover struct {
	autoApprove   bool
	prompts       chan approvalPrompt
	approvedTools map[string]agent.ApprovalDecision
}

func (a *tuiApprover) Approve(req agent.ApprovalRequest) bool {
	return a.ApproveWithOptions(req) != agent.ApprovalDeny
}

func (a *tuiApprover) ApproveWithOptions(req agent.ApprovalRequest) agent.ApprovalDecision {
	if a.autoApprove {
		return agent.ApprovalAlways
	}
	if a.approvedTools != nil {
		if decision, ok := a.approvedTools[req.Tool]; ok {
			return decision
		}
	}
	decisionCh := make(chan agent.ApprovalDecision, 1)
	select {
	case a.prompts <- approvalPrompt{req: req, decision: decisionCh}:
		select {
		case result := <-decisionCh:
			if result == agent.ApprovalSession || result == agent.ApprovalAlways {
				if a.approvedTools == nil {
					a.approvedTools = map[string]agent.ApprovalDecision{}
				}
				a.approvedTools[req.Tool] = result
			}
			return result
		case <-time.After(approvalTimeout):
			return agent.ApprovalDeny
		}
	default:
		return agent.ApprovalDeny
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
	return renderEventWithDetails(event, false)
}

func renderEventWithDetails(event agent.Event, collapsed bool) string {
	switch event.Type {
	case "context_compacted":
		return helpStyle.Render("· " + compact(event.Summary, 120))
	case "tool_requested":
		text := toolStyle.Render(fmt.Sprintf("→ Tool requested [%s]: %s", event.Effect, compact(event.Summary, 500)))
		if event.Detail != "" && !collapsed {
			text += "\n" + diffStyle.Render(compact(event.Detail, 1000))
		} else if event.Detail != "" {
			text += "\n" + helpStyle.Render("  [details collapsed; Ctrl+O to expand]")
		}
		return text
	case "approval_requested":
		text := approvalStyle.Render(fmt.Sprintf("! Approval requested [%s]", event.Effect))
		if event.Detail != "" && !collapsed {
			text += "\n" + diffStyle.Render(compact(event.Detail, 1000))
		} else if event.Detail != "" {
			text += "\n" + helpStyle.Render("  [details collapsed; Ctrl+O to expand]")
		}
		return text
	case "approval_approved":
		kind := event.Detail
		if kind == "" {
			kind = "once"
		}
		return approvalStyle.Render(fmt.Sprintf("✓ Approval granted [%s, %s]", event.Effect, kind))
	case "approval_denied":
		return approvalStyle.Render(fmt.Sprintf("× Approval denied [%s]", event.Effect))
	case "tool_completed":
		return toolStyle.Render(fmt.Sprintf("✓ Tool completed: %s", compact(event.Summary, 500)))
	case "tool_failed":
		if event.Error != "" {
			return errorStyle.Render(fmt.Sprintf("× Tool failed: %s", compact(event.Error, 500)))
		}
		return errorStyle.Render(fmt.Sprintf("× Tool failed: %s", compact(event.Summary, 500)))
	default:
		return toolStyle.Render(compact(event.Summary, 500))
	}
}

func renderApproval(req agent.ApprovalRequest) string {
	return renderApprovalWithOptions(req, false)
}

func renderApprovalWithOptions(req agent.ApprovalRequest, expanded bool) string {
	tool := req.Tool
	if tool == "" {
		fields := strings.Fields(req.Summary)
		if len(fields) > 0 {
			tool = fields[0]
		}
	}
	files := affectedFiles(req.Summary, req.Diff)
	risk := "moderate"
	if req.Kind == "destructive" {
		risk = "critical"
	} else if req.Kind == "network" || req.Kind == "shell" {
		risk = "high"
	}
	text := fmt.Sprintf("%s\nTool: %s\nRisk: %s", approvalStyle.Render("Approval required • "+req.Kind), tool, risk)
	if len(files) > 0 {
		text += "\nFiles: " + strings.Join(files, ", ")
	}
	text += "\n" + compact(req.Summary, 4000)
	if req.Diff != "" {
		limit := 1200
		if expanded {
			limit = 8000
		}
		text += "\n" + diffStyle.Render(compact(req.Diff, limit))
		if !expanded {
			text += "\n" + helpStyle.Render("Ctrl+O expands the full diff.")
		}
	}
	text += "\n" + helpStyle.Render("y to approve once  •  2 for session  •  3 always for this tool  •  n deny")
	return text
}

func affectedFiles(summary, diff string) []string {
	text := summary + "\n" + diff
	seen := map[string]bool{}
	var files []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"+++ b/", "--- a/", "path:", "file:"} {
			if strings.HasPrefix(strings.ToLower(line), prefix) {
				file := strings.TrimSpace(line[len(prefix):])
				if file != "" && file != "/dev/null" && !seen[file] {
					seen[file] = true
					files = append(files, file)
				}
			}
		}
	}
	if len(files) == 0 {
		fields := strings.Fields(summary)
		if len(fields) > 1 && (strings.HasSuffix(fields[0], "_file") || fields[0] == "write_patch") {
			files = append(files, strings.Trim(fields[1], "\"'"))
		}
	}
	return files
}

func compact(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	cut := limit
	for !utf8.ValidString(s[:cut]) && cut > 0 {
		cut--
	}
	return s[:cut] + "\n... truncated ..."
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

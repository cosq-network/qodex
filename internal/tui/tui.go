package tui

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

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
	agent        *agent.Agent
	input        textarea.Model
	viewport     viewport.Model
	spinner      spinner.Model
	history      []string
	busy         bool
	busyLabel    string
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
	req   agent.ApprovalRequest
	reply chan bool
}

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
		m.viewport.Width = max(20, msg.Width)
		m.viewport.Height = max(5, msg.Height-m.input.Height()-4)
		m.refresh()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.pending != nil {
				m.pending.reply <- false
				m.pending = nil
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
			if m.pending != nil {
				m.pending.reply <- false
				m.pending = nil
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
				prompt = actionPrompt
			}
			m.busy = true
			m.busyLabel = "Running agent"
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

func slashHelp() string {
	return helpStyle.Render(strings.TrimSpace(`Slash commands:

/help              Show this help
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
	} else if m.pending != nil {
		status = approvalStyle.Render("Approval pending  •  y approve  •  n deny  •  Esc cancel")
	} else {
		status = statusBarStyle.Render("Enter submit  •  Ctrl+J newline  •  @ files  •  / commands  •  Ctrl+C quit")
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

	inputWidth := max(20, m.width-2)
	if m.width == 0 {
		inputWidth = 78
	}
	inputView := inputFrameStyle.Width(inputWidth).Render(m.input.View())
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.viewport.View(),
		autoView,
		status,
		inputView,
	)
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
	default:
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
		return helpStyle.Render("· " + compact(event.Summary, 120))
	case "tool_requested":
		text := toolStyle.Render(fmt.Sprintf("→ Tool requested [%s]: %s", event.Effect, compact(event.Summary, 500)))
		if event.Detail != "" {
			text += "\n" + diffStyle.Render(compact(event.Detail, 1000))
		}
		return text
	case "approval_requested":
		text := approvalStyle.Render(fmt.Sprintf("! Approval requested [%s]", event.Effect))
		if event.Detail != "" {
			text += "\n" + diffStyle.Render(compact(event.Detail, 1000))
		}
		return text
	case "approval_approved":
		return approvalStyle.Render(fmt.Sprintf("✓ Approval granted [%s]", event.Effect))
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

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/benoybose/qodex/internal/config"
	"github.com/benoybose/qodex/internal/model"
	"github.com/benoybose/qodex/internal/skills"
	"github.com/benoybose/qodex/internal/store"
	"github.com/benoybose/qodex/internal/tools"
)

type PlanState struct {
	CurrentTask    string
	FilesInspected []string
	ActionsTaken   []string
}

type Options struct {
	Config      config.Config
	Client      *model.Client
	Tools       *tools.Registry
	Store       *store.Store
	Skills      []skills.Skill
	Approver    Approver
	Observer    Observer
	MaxSteps    int
	SessionID   int64
	DebugWriter io.Writer // optional; if set, diagnostic messages are written here too
}

type Agent struct {
	cfg            config.Config
	client         *model.Client
	tools          *tools.Registry
	store          *store.Store
	skills         []skills.Skill
	approver       Approver
	observer       Observer
	maxSteps       int
	sessionID      int64
	messages       []model.Message
	streamCallback func(string)
	streaming      bool
	planState      PlanState
	allowedTools   []string
	selectedSkills []skills.Skill
	debugWriter    io.Writer
	probeCancel    context.CancelFunc
}

type ApprovalRequest struct {
	Kind    string
	Summary string
	Diff    string
}

type Approver interface {
	Approve(ApprovalRequest) bool
}

type ApproverFunc func(ApprovalRequest) bool

func (f ApproverFunc) Approve(req ApprovalRequest) bool {
	return f(req)
}

type Event struct {
	Type    string
	Tool    string
	Effect  string
	Summary string
	Error   string
	Detail  string
}

type Observer interface {
	OnEvent(Event)
}

type ObserverFunc func(Event)

func (f ObserverFunc) OnEvent(event Event) {
	f(event)
}

func New(opts Options) *Agent {
	maxSteps := opts.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 12
	}
	return &Agent{
		cfg:         opts.Config,
		client:      opts.Client,
		tools:       opts.Tools,
		store:       opts.Store,
		skills:      opts.Skills,
		approver:    opts.Approver,
		observer:    opts.Observer,
		maxSteps:    maxSteps,
		sessionID:   opts.SessionID,
		debugWriter: opts.DebugWriter,
	}
}

func (a *Agent) ProjectRoot() string {
	return a.cfg.ProjectRoot
}

func (a *Agent) SetApprover(approver Approver) {
	a.approver = approver
}

func (a *Agent) SetObserver(observer Observer) {
	a.observer = observer
}

func (a *Agent) SetStreamCallback(cb func(string)) {
	a.streamCallback = cb
}

func (a *Agent) SetStreaming(enabled bool) {
	a.streaming = enabled
}

func (a *Agent) SetProbeCancel(cancel context.CancelFunc) {
	a.probeCancel = cancel
}

func (a *Agent) CancelProbe() {
	if a.probeCancel != nil {
		a.probeCancel()
	}
}

func (a *Agent) Run(ctx context.Context, prompt string) (result string, err error) {
	a.logDebug("agent run: prompt=%q tool_calls=%s max_steps=%d", prompt, a.cfg.Agent.ToolCalls, a.maxSteps)
	defer func() {
		if r := recover(); r != nil {
			a.logError("panic in agent loop: %v", r)
			err = fmt.Errorf("agent panicked: %v", r)
		}
	}()
	a.planState.CurrentTask = prompt
	if a.sessionID == 0 {
		title := prompt
		if len(title) > 80 {
			title = title[:80]
		}
		id, err := a.store.CreateSession(ctx, a.cfg.ProjectRoot, title, a.cfg.Model.Model, a.cfg.Runtime.Backend)
		if err != nil {
			return "", err
		}
		a.sessionID = id
	}
	if len(a.messages) == 0 {
		a.selectSkills(ctx, prompt)
		a.messages = append(a.messages, model.Message{Role: "system", Content: a.systemPrompt(prompt)})
		if a.sessionID != 0 {
			stored, err := a.store.ListMessages(ctx, a.sessionID)
			if err != nil {
				return "", err
			}
			for _, msg := range stored {
				a.messages = append(a.messages, model.Message{Role: msg.Role, Content: msg.Content})
			}
			toolCalls, err := a.store.ListToolCalls(ctx, a.sessionID)
			if err == nil && len(toolCalls) > 0 {
				var summary strings.Builder
				summary.WriteString("Tool call state from prior session:\n")
				for _, tc := range toolCalls {
					summary.WriteString(fmt.Sprintf("- %s: %s [%s]", tc.Name, tc.Arguments, tc.Status))
					if tc.Result != nil {
						if tc.Result.Error != "" {
							summary.WriteString(fmt.Sprintf(" error=%s", tc.Result.Error))
						}
					}
					summary.WriteString("\n")
				}
				a.messages = append(a.messages, model.Message{Role: "system", Content: summary.String()})
			}
		}
	}
	a.messages = append(a.messages, model.Message{Role: "user", Content: prompt})
	if err := a.store.AddMessage(ctx, a.sessionID, "user", prompt); err != nil {
		a.logError("failed to persist user message: %v", err)
	}

	useNative := a.cfg.Agent.ToolCalls == "native"

	for step := 0; step < a.maxSteps; step++ {
		if useNative {
			tools := a.tools.ToolSchemas()
			res, err := a.chatWithTools(ctx, tools)
			if err != nil {
				return "", err
			}
			if len(res.ToolCalls) > 0 {
				a.logDebug("model returned %d native tool calls", len(res.ToolCalls))
				for _, tc := range res.ToolCalls {
					a.logDebug("  tool_call: %s args=%s", tc.Function.Name, string(tc.Function.Arguments))
					resultText, err := a.executeTool(ctx, toolCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					})
					if err != nil {
						resultText = fmt.Sprintf(`{"ok":false,"summary":"tool failed","content":%q}`, err.Error())
					}
					a.messages = append(a.messages, model.Message{
						Role:    "assistant",
						Content: res.Content,
						ToolCalls: []model.ToolCall{{
							ID:   tc.ID,
							Type: tc.Type,
							Function: model.ToolCallFunction{
								Name:      tc.Function.Name,
								Arguments: tc.Function.Arguments,
							},
						}},
					})
					a.messages = append(a.messages, model.Message{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content:    resultText,
					})
					if err := a.store.AddMessage(ctx, a.sessionID, "assistant", res.Content); err != nil {
						a.logError("failed to persist assistant message: %v", err)
					}
					if err := a.store.AddMessage(ctx, a.sessionID, "tool", resultText); err != nil {
						a.logError("failed to persist tool result message: %v", err)
					}
				}
				continue
			}
			if res.Content != "" {
				a.messages = append(a.messages, model.Message{Role: "assistant", Content: res.Content})
				if err := a.store.AddMessage(ctx, a.sessionID, "assistant", res.Content); err != nil {
					a.logError("failed to persist assistant message: %v", err)
				}
				return res.Content, nil
			}
			return "", fmt.Errorf("agent: empty response from model")
		}

		content, err := a.chat(ctx)
		if err != nil {
			return "", err
		}
		a.logDebug("model response: %s", debugTruncate(content, 500))
		call, ok, validationErr := parseToolCallDetailed(content)
		if validationErr != nil {
			a.logDebug("tool call validation error: %v", validationErr)
			a.messages = append(a.messages, model.Message{Role: "assistant", Content: content})
			a.messages = append(a.messages, model.Message{Role: "user", Content: "Tool call validation error: " + validationErr.Error() + "\nRespond with exactly one valid tool_call JSON object or a final answer."})
			continue
		}
		if ok {
			a.logDebug("parsed tool call: %s args=%s", call.Name, string(call.Arguments))
			resultText, err := a.executeTool(ctx, call)
			if err != nil {
				resultText = fmt.Sprintf(`{"ok":false,"summary":"tool failed","content":%q}`, err.Error())
			}
			a.messages = append(a.messages, model.Message{Role: "assistant", Content: content})
			a.messages = append(a.messages, model.Message{Role: "tool", Content: resultText})
			continue
		}
		a.messages = append(a.messages, model.Message{Role: "assistant", Content: content})
		if err := a.store.AddMessage(ctx, a.sessionID, "assistant", content); err != nil {
			a.logError("failed to persist assistant message: %v", err)
		}
		return content, nil
	}
	return "", fmt.Errorf("agent stopped after %d steps", a.maxSteps)
}

func (a *Agent) estimateTokens(msg model.Message) int {
	return len(msg.Content) / 4
}

func (a *Agent) compactContext() {
	if a.cfg.Runtime.ContextTokens <= 0 {
		return
	}
	total := 0
	for _, msg := range a.messages {
		total += a.estimateTokens(msg)
	}
	threshold := int(float64(a.cfg.Runtime.ContextTokens) * 0.7)
	if total <= threshold {
		return
	}
	keepSystem := 0
	for i, msg := range a.messages {
		if msg.Role == "system" {
			keepSystem = i
			break
		}
	}
	systemMsgs := a.messages[:keepSystem+1]
	rest := a.messages[keepSystem+1:]
	if len(rest) <= 4 {
		return
	}
	keepRecent := 8
	if len(rest) < keepRecent {
		keepRecent = len(rest)
	}
	recent := rest[len(rest)-keepRecent:]
	full := make([]model.Message, 0, len(systemMsgs)+keepRecent+1)
	full = append(full, systemMsgs...)
	compacted := "Previous conversation context was compacted to fit within the model's context window."
	full = append(full, model.Message{Role: "system", Content: compacted})
	full = append(full, recent...)
	a.messages = full
	a.emit(Event{Type: "context_compacted", Summary: "Conversation context was compacted to fit within the model's context window."})
}

func (a *Agent) chat(ctx context.Context) (string, error) {
	a.compactContext()
	if !a.streaming || a.streamCallback == nil {
		return a.client.Chat(ctx, a.messages, a.cfg.Runtime.Temperature, a.cfg.Runtime.TopP)
	}
	ch, err := a.client.ChatStream(ctx, a.messages, a.cfg.Runtime.Temperature, a.cfg.Runtime.TopP)
	if err != nil {
		return "", err
	}
	var full strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			return full.String(), chunk.Err
		}
		full.WriteString(chunk.Content)
		a.streamCallback(chunk.Content)
	}
	return full.String(), nil
}

func (a *Agent) chatWithTools(ctx context.Context, tools []model.ToolSchema) (*model.ResponseMessage, error) {
	a.compactContext()
	return a.client.ChatWithTools(ctx, a.messages, a.cfg.Runtime.Temperature, a.cfg.Runtime.TopP, tools)
}

func (a *Agent) selectSkills(ctx context.Context, userPrompt string) {
	if a.cfg.Agent.SkillRouting == "model" && a.client != nil {
		ask := func(ctx context.Context, msg string) (string, error) {
			msgs := []model.Message{{Role: "user", Content: msg}}
			return a.client.Chat(ctx, msgs, 0.0, 1.0)
		}
		modelSelected, err := skills.SelectViaModel(ctx, a.skills, userPrompt, ask)
		if err == nil && modelSelected != nil {
			a.selectedSkills = modelSelected
			a.allowedTools = skills.AllowedTools(modelSelected)
			return
		}
	}
	a.selectedSkills = skills.Select(a.skills, userPrompt)
	a.allowedTools = skills.AllowedTools(a.selectedSkills)
}

func (a *Agent) systemPrompt(userPrompt string) string {
	var b strings.Builder
	b.WriteString("You are Qodex, a local coding agent running on the user's machine.\n")
	b.WriteString("You must not claim to have read, changed, or executed anything unless a tool result proves it.\n")
	if a.cfg.Agent.ToolCalls == "native" {
		b.WriteString("Use the available tools when you need to interact with the project. Call one tool at a time.\n")
		b.WriteString("When you have enough information, respond with the final answer.\n")
	} else {
		b.WriteString("When you need a tool, respond with exactly one JSON object and no Markdown:\n")
		b.WriteString(`{"tool_call":{"name":"read_file","arguments":{"path":"README.md"}}}` + "\n")
		b.WriteString("When you have enough information, respond normally with the final answer.\n")
	}
	b.WriteString("Prefer narrow reads and searches before edits. Explain risky actions before requesting shell commands.\n\n")
	b.WriteString("Current task: ")
	b.WriteString(a.planState.CurrentTask)
	b.WriteString("\n")
	if len(a.planState.FilesInspected) > 0 {
		b.WriteString("Files already inspected:\n")
		for _, f := range a.planState.FilesInspected {
			b.WriteString("- ")
			b.WriteString(f)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	toolPrompt := a.tools.Prompt()
	if a.allowedTools != nil {
		toolPrompt = filterToolPrompt(toolPrompt, a.allowedTools)
	}
	b.WriteString(toolPrompt)
	if rendered := skills.RenderSliced(a.selectedSkills, userPrompt, 8000); rendered != "" {
		b.WriteString("\n")
		b.WriteString(rendered)
	}
	if scripts := skills.Scripts(a.selectedSkills); len(scripts) > 0 {
		b.WriteString("\nPre-approved scripts — use run_script with the description to execute without approval:\n")
		for _, s := range scripts {
			b.WriteString("- ")
			b.WriteString(s.Description)
			b.WriteString(": `")
			b.WriteString(s.Command)
			b.WriteString("`\n")
		}
	}
	return b.String()
}

func filterToolPrompt(prompt string, allowed []string) string {
	allowedSet := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = true
	}
	var out strings.Builder
	lines := strings.Split(prompt, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "- ") {
			fields := strings.Fields(line[2:])
			if len(fields) > 0 {
				toolName := strings.TrimSuffix(fields[0], ":")
				if !allowedSet[toolName] {
					continue
				}
			}
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

type toolCallEnvelope struct {
	ToolCall toolCall `json:"tool_call"`
}

type toolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func parseToolCall(content string) (toolCall, bool) {
	call, ok, _ := parseToolCallDetailed(content)
	return call, ok
}

func parseToolCallDetailed(content string) (toolCall, bool, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var env toolCallEnvelope
	if err := json.Unmarshal([]byte(content), &env); err == nil && env.ToolCall.Name != "" {
		return env.ToolCall, true, nil
	} else if looksLikeToolCall(content) && err != nil {
		return toolCall{}, false, fmt.Errorf("invalid tool_call JSON: %w", err)
	}

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(content[start:end+1]), &env); err == nil && env.ToolCall.Name != "" {
			return env.ToolCall, true, nil
		} else if looksLikeToolCall(content[start : end+1]) {
			return toolCall{}, false, fmt.Errorf("invalid embedded tool_call JSON: %w", err)
		}
	}
	return toolCall{}, false, nil
}

func looksLikeToolCall(content string) bool {
	return strings.Contains(content, "tool_call") || strings.Contains(content, `"arguments"`) && strings.Contains(content, `"name"`)
}

func (a *Agent) executeTool(ctx context.Context, call toolCall) (string, error) {
	if call.Name == "run_script" {
		return a.executeScript(ctx, call)
	}

	if a.allowedTools != nil {
		allowed := false
		for _, name := range a.allowedTools {
			if call.Name == name {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", fmt.Errorf("tool %q is not allowed by active skills", call.Name)
		}
	}

	tool, ok := a.tools.Get(call.Name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", call.Name)
	}
	a.logDebug("execute tool: %s args=%s", call.Name, string(call.Arguments))
	callID, err := a.store.AddToolCall(ctx, a.sessionID, call.Name, string(call.Arguments), "requested")
	if err != nil {
		a.logError("failed to persist tool call: %v", err)
	}

	a.planState.ActionsTaken = append(a.planState.ActionsTaken, call.Name+" "+string(call.Arguments))
	if call.Name == "read_file" {
		var args struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(call.Arguments, &args) == nil && args.Path != "" {
			a.planState.FilesInspected = append(a.planState.FilesInspected, args.Path)
		}
	}

	effect := tool.Effect
	if call.Name == "run_command" && tools.IsNetworkCommand(call.Arguments) {
		effect = "network"
	}
	if (call.Name == "npm_command" || call.Name == "npx_command") && tools.IsNpmCommandNetwork(call.Arguments) {
		effect = "network"
	}
	if call.Name == "npx_command" && tools.IsNpxCommandNetwork(call.Arguments) {
		effect = "network"
	}
	diff, _ := a.tools.DiffPreview(call.Name, call.Arguments)
	summary := call.Name + " " + string(call.Arguments)
	a.emit(Event{Type: "tool_requested", Tool: call.Name, Effect: effect, Summary: summary, Detail: diff})
	approved, denied := a.approvalPolicy(effect, call.Arguments)
	isApprovalRequired := effect == "write" || effect == "shell" || effect == "network" || effect == "destructive"
	if isApprovalRequired {
		if denied {
			a.emit(Event{Type: "approval_denied", Tool: call.Name, Effect: effect, Summary: summary})
			if err := a.store.AddToolResult(ctx, callID, "", "denied by policy"); err != nil {
				a.logError("failed to persist tool result: %v", err)
			}
			if err := a.store.AddApproval(ctx, a.sessionID, callID, call.Name, effect, summary, false); err != nil {
				a.logError("failed to persist approval: %v", err)
			}
			return `{"ok":false,"summary":"approval denied by policy"}`, nil
		}
		if approved {
			a.emit(Event{Type: "approval_auto_approved", Tool: call.Name, Effect: effect, Summary: summary})
			if err := a.store.AddApproval(ctx, a.sessionID, callID, call.Name, effect, summary, true); err != nil {
				a.logError("failed to persist approval: %v", err)
			}
		} else {
			a.emit(Event{Type: "approval_requested", Tool: call.Name, Effect: effect, Summary: summary, Detail: diff})
			userApproved := a.approver != nil && a.approver.Approve(ApprovalRequest{Kind: effect, Summary: summary, Diff: diff})
			if !userApproved {
				a.emit(Event{Type: "approval_denied", Tool: call.Name, Effect: effect, Summary: summary})
				if err := a.store.AddToolResult(ctx, callID, "", "approval denied"); err != nil {
					a.logError("failed to persist tool result: %v", err)
				}
				if err := a.store.AddApproval(ctx, a.sessionID, callID, call.Name, effect, summary, false); err != nil {
					a.logError("failed to persist approval: %v", err)
				}
				return `{"ok":false,"summary":"approval denied"}`, nil
			}
			a.emit(Event{Type: "approval_approved", Tool: call.Name, Effect: effect, Summary: summary})
			if err := a.store.AddApproval(ctx, a.sessionID, callID, call.Name, effect, summary, true); err != nil {
				a.logError("failed to persist approval: %v", err)
			}
		}
	}

	res, err := tool.Execute(ctx, call.Arguments)
	if len(res.Content) > store.ArtifactThreshold {
		summary := res.Summary
		if summary == "" {
			summary = fmt.Sprintf("Large output from %s (%d bytes)", call.Name, len(res.Content))
		}
		artifactID, aerr := a.store.SaveArtifact(ctx, a.sessionID, callID, call.Name, summary, res.Content, "text")
		if aerr == nil {
			res.Content = fmt.Sprintf("[Output stored as artifact #%d. Summary: %s]", artifactID, summary)
		}
	}
	raw, marshalErr := json.Marshal(res)
	if marshalErr != nil {
		a.logError("failed to marshal tool result: %v", marshalErr)
	}
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	if err := a.store.AddToolResult(ctx, callID, string(raw), errText); err != nil {
		a.logError("failed to persist tool result: %v", err)
	}
	if err != nil {
		a.emit(Event{Type: "tool_failed", Tool: call.Name, Effect: effect, Summary: res.Summary, Error: err.Error()})
	} else {
		a.emit(Event{Type: "tool_completed", Tool: call.Name, Effect: effect, Summary: res.Summary})
	}
	if err != nil && res.Content == "" {
		return string(raw), err
	}
	return string(raw), nil
}

func (a *Agent) executeScript(ctx context.Context, call toolCall) (string, error) {
	var args struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil || args.Description == "" {
		return "", fmt.Errorf("run_script requires a \"description\" field")
	}

	var found *skills.Script
	var skillName string
	for _, skill := range a.selectedSkills {
		for _, script := range skill.Meta.Scripts {
			desc := strings.ToLower(script.Description)
			cmd := strings.ToLower(script.Command)
			needle := strings.ToLower(args.Description)
			if strings.Contains(desc, needle) || strings.Contains(cmd, needle) {
				s := script
				found = &s
				skillName = skill.Name
				break
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		return "", fmt.Errorf("no script matching %q found in active skills", args.Description)
	}

	callID, err := a.store.AddToolCall(ctx, a.sessionID, "run_script", string(call.Arguments), "approved")
	if err != nil {
		a.logError("failed to persist tool call: %v", err)
	}

	effect := "shell"
	summary := fmt.Sprintf("run_script %q (from skill %q)", found.Description, skillName)
	a.emit(Event{Type: "tool_requested", Tool: "run_script", Effect: effect, Summary: summary})
	a.emit(Event{Type: "approval_requested", Tool: "run_script", Effect: effect, Summary: summary, Detail: "pre-approved script: " + found.Command})
	a.emit(Event{Type: "approval_approved", Tool: "run_script", Effect: effect, Summary: summary})
	if err := a.store.AddApproval(ctx, a.sessionID, callID, "run_script", effect, summary, true); err != nil {
		a.logError("failed to persist approval: %v", err)
	}

	shell, shellArgs := tools.ShellCommand(found.Command)
	cmd := exec.CommandContext(ctx, shell, shellArgs...)
	cmd.Dir = a.cfg.ProjectRoot
	output, err := cmd.CombinedOutput()

	result := tools.Result{
		OK:      err == nil,
		Summary: fmt.Sprintf("script %q from skill %q executed", found.Description, skillName),
		Content: string(output),
		Metadata: map[string]interface{}{
			"script":    found.Description,
			"skill":     skillName,
			"command":   found.Command,
			"provenance": fmt.Sprintf("skill:%s/script:%s", skillName, found.Description),
		},
	}
	raw, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		a.logError("failed to marshal tool result: %v", marshalErr)
	}
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	if err := a.store.AddToolResult(ctx, callID, string(raw), errText); err != nil {
		a.logError("failed to persist tool result: %v", err)
	}
	if err != nil {
		a.emit(Event{Type: "tool_failed", Tool: "run_script", Effect: effect, Summary: result.Summary, Error: err.Error()})
	} else {
		a.emit(Event{Type: "tool_completed", Tool: "run_script", Effect: effect, Summary: result.Summary})
	}
	return string(raw), nil
}

func (a *Agent) logError(format string, args ...interface{}) {
	msg := fmt.Sprintf("[qodex] "+format, args...)
	fmt.Fprintln(os.Stderr, msg)
	if a.debugWriter != nil {
		ts := time.Now().UTC().Format(time.RFC3339)
		fmt.Fprintf(a.debugWriter, "%s ERROR %s\n", ts, msg)
	}
}

func (a *Agent) logDebug(format string, args ...interface{}) {
	if a.debugWriter == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().UTC().Format(time.RFC3339)
	fmt.Fprintf(a.debugWriter, "%s DEBUG %s\n", ts, msg)
}

func (a *Agent) emit(event Event) {
	if a.observer != nil {
		a.observer.OnEvent(event)
	}
}

func (a *Agent) approvalPolicy(effect string, raw json.RawMessage) (approve bool, deny bool) {
	cfg := a.cfg.Approval
	if cfg.AutoApprove {
		return true, false
	}
	switch effect {
	case "write":
		return policyFromConfig(cfg.WriteFiles)
	case "shell":
		return policyFromConfig(cfg.RunCommands)
	case "network":
		if tools.IsNetworkCommand(raw) {
			return policyFromConfig(cfg.Network)
		}
		return policyFromConfig(cfg.RunCommands)
	case "destructive":
		return false, true
	}
	return true, false
}

func policyFromConfig(val string) (approve bool, deny bool) {
	switch val {
	case "allow":
		return true, false
	case "deny":
		return false, true
	}
	return false, false
}

func debugTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

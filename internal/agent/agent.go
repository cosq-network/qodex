package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/benoybose/qodex/internal/config"
	"github.com/benoybose/qodex/internal/credentials"
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

type Progress struct {
	CurrentTask    string
	FilesInspected []string
	ActionsTaken   []string
	ActiveSkills   []string
	ActiveTools    []string
	CurrentStep    int
	MaxSteps       int
	ContextTokens  int
	ContextLimit   int
}

type Options struct {
	Config          config.Config
	Client          *model.Client
	Tools           *tools.Registry
	Store           *store.Store
	Skills          []skills.Skill
	Instructions    []skills.Instruction
	MCPToolPolicies map[string]string
	Approver        Approver
	Observer        Observer
	MaxSteps        int
	SessionID       int64
	DebugWriter     io.Writer // optional; if set, diagnostic messages are written here too
}

type Agent struct {
	cfg              config.Config
	client           *model.Client
	tools            *tools.Registry
	store            *store.Store
	skills           []skills.Skill
	approver         Approver
	observer         Observer
	maxSteps         int
	sessionID        int64
	messages         []model.Message
	streamCallback   func(string)
	streaming        bool
	planState        PlanState
	currentStep      int
	allowedTools     []string
	activeTools      []string
	selectedSkills   []skills.Skill
	selectionMatches []skills.Match
	instructions     []skills.Instruction
	mcpToolPolicies  map[string]string
	debugWriter      io.Writer
	probeCancel      context.CancelFunc
}

type ApprovalRequest struct {
	Tool    string
	Kind    string
	Summary string
	Diff    string
}

type Approver interface {
	Approve(ApprovalRequest) bool
}

type ApprovalDecision int

const (
	ApprovalDeny ApprovalDecision = iota
	ApprovalOnce
	ApprovalSession
	ApprovalAlways
)

type AdvancedApprover interface {
	ApproveWithOptions(ApprovalRequest) ApprovalDecision
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
		cfg:             opts.Config,
		client:          opts.Client,
		tools:           opts.Tools,
		store:           opts.Store,
		skills:          opts.Skills,
		instructions:    opts.Instructions,
		mcpToolPolicies: opts.MCPToolPolicies,
		approver:        opts.Approver,
		observer:        opts.Observer,
		maxSteps:        maxSteps,
		sessionID:       opts.SessionID,
		debugWriter:     opts.DebugWriter,
	}
}

func (a *Agent) ProjectRoot() string {
	return a.cfg.ProjectRoot
}

func (a *Agent) Config() config.Config {
	return a.cfg
}

func messageMetadata(msg model.Message) string {
	if len(msg.ToolCalls) == 0 && msg.ToolCallID == "" {
		return ""
	}
	data, err := json.Marshal(struct {
		ToolCallID string           `json:"tool_call_id,omitempty"`
		ToolCalls  []model.ToolCall `json:"tool_calls,omitempty"`
	}{ToolCallID: msg.ToolCallID, ToolCalls: msg.ToolCalls})
	if err != nil {
		return ""
	}
	return string(data)
}

func (a *Agent) persistSessionContext(ctx context.Context) error {
	if a.store == nil || a.sessionID == 0 {
		return nil
	}
	names := make([]string, 0, len(a.selectedSkills))
	for _, skill := range a.selectedSkills {
		names = append(names, skill.Name)
	}
	skillsJSON, err := json.Marshal(names)
	if err != nil {
		return err
	}
	toolsJSON, err := json.Marshal(a.activeTools)
	if err != nil {
		return err
	}
	return a.store.UpdateSessionContext(ctx, a.sessionID, string(skillsJSON), string(toolsJSON), a.cfg.Agent.ToolCalls)
}

// ConfigureModel switches the active model endpoint and persists the
// non-secret model settings in the project config. API keys remain in the
// environment and are referenced only by token_env.
func (a *Agent) ConfigureModel(ctx context.Context, modelCfg config.ModelConfig, backend string) error {
	if strings.TrimSpace(modelCfg.BaseURL) == "" || strings.TrimSpace(modelCfg.Model) == "" {
		return fmt.Errorf("model endpoint and model name are required")
	}
	if err := config.SetProjectValue(a.cfg.ProjectRoot, "model.base_url", modelCfg.BaseURL); err != nil {
		return fmt.Errorf("persist model.base_url: %w", err)
	}
	if err := config.SetProjectValue(a.cfg.ProjectRoot, "model.model", modelCfg.Model); err != nil {
		return fmt.Errorf("persist model.model: %w", err)
	}
	if err := config.SetProjectValue(a.cfg.ProjectRoot, "runtime.backend", backend); err != nil {
		return fmt.Errorf("persist runtime.backend: %w", err)
	}
	if modelCfg.Auth.Type == "" || modelCfg.Auth.Type == "none" {
		if err := config.SetProjectValue(a.cfg.ProjectRoot, "model.auth.type", "none"); err != nil {
			return fmt.Errorf("persist model.auth.type: %w", err)
		}
	}
	if err := config.SetProjectValue(a.cfg.ProjectRoot, "model.auth.token_env", modelCfg.Auth.TokenEnv); err != nil {
		return fmt.Errorf("persist model.auth.token_env: %w", err)
	}
	if err := config.SetProjectValue(a.cfg.ProjectRoot, "model.auth.header", modelCfg.Auth.Header); err != nil {
		return fmt.Errorf("persist model.auth.header: %w", err)
	}
	if modelCfg.Auth.Type != "" && modelCfg.Auth.Type != "none" {
		if err := config.SetProjectValue(a.cfg.ProjectRoot, "model.auth.type", modelCfg.Auth.Type); err != nil {
			return fmt.Errorf("persist model.auth.type: %w", err)
		}
	}
	a.cfg.Model = modelCfg
	a.cfg.Runtime.Backend = backend
	toolCalls := "prompt"
	baseURL := strings.ToLower(modelCfg.BaseURL)
	if backend == string(model.BackendExternal) && (strings.Contains(baseURL, "api.groq.com") || strings.Contains(baseURL, "openrouter.ai")) {
		toolCalls = "native"
	}
	if err := config.SetProjectValue(a.cfg.ProjectRoot, "agent.tool_calls", toolCalls); err != nil {
		return fmt.Errorf("persist agent.tool_calls: %w", err)
	}
	a.cfg.Agent.ToolCalls = toolCalls
	a.client.BaseURL = strings.TrimRight(modelCfg.BaseURL, "/")
	a.client.Model = modelCfg.Model
	a.client.SetAuthToken("")
	a.client.SetAuth(modelCfg.Auth.Type, modelCfg.Auth.TokenEnv, modelCfg.Auth.Header)
	if modelCfg.Auth.TokenEnv != "" && modelCfg.Auth.Type != "" && modelCfg.Auth.Type != "none" {
		if token, err := credentials.Load(a.cfg.ProjectRoot, modelCfg.Auth.TokenEnv); err == nil && strings.TrimSpace(token) != "" {
			a.client.SetAuthToken(token)
		}
	}
	return nil
}

func (a *Agent) SessionID() int64 { return a.sessionID }

func (a *Agent) ExportSession(ctx context.Context) (store.ExportData, error) {
	if a.store == nil || a.sessionID == 0 {
		return store.ExportData{}, fmt.Errorf("no active session to export")
	}
	return a.store.ExportSession(ctx, a.sessionID)
}

func (a *Agent) Progress() Progress {
	active := make([]string, 0, len(a.selectedSkills))
	for _, skill := range a.selectedSkills {
		active = append(active, skill.Name)
	}
	tokens := 0
	for _, msg := range a.messages {
		tokens += a.estimateTokens(msg)
	}
	return Progress{
		CurrentTask:    a.planState.CurrentTask,
		FilesInspected: append([]string(nil), a.planState.FilesInspected...),
		ActionsTaken:   append([]string(nil), a.planState.ActionsTaken...),
		ActiveSkills:   active,
		ActiveTools:    append([]string(nil), a.activeTools...),
		CurrentStep:    a.currentStep,
		MaxSteps:       a.maxSteps,
		ContextTokens:  tokens,
		ContextLimit:   a.cfg.Runtime.ContextTokens,
	}
}

func (a *Agent) PlanSummary() string {
	var b strings.Builder
	b.WriteString("Plan state\n")
	if a.planState.CurrentTask == "" {
		b.WriteString("Task: none\n")
	} else {
		b.WriteString("Task: ")
		b.WriteString(a.planState.CurrentTask)
		b.WriteByte('\n')
	}
	if len(a.planState.FilesInspected) > 0 {
		b.WriteString("Files inspected:\n")
		for _, file := range a.planState.FilesInspected {
			b.WriteString("- ")
			b.WriteString(file)
			b.WriteByte('\n')
		}
	}
	if len(a.planState.ActionsTaken) > 0 {
		b.WriteString("Actions taken:\n")
		for _, action := range a.planState.ActionsTaken {
			b.WriteString("- ")
			b.WriteString(action)
			b.WriteByte('\n')
		}
	}
	if len(a.planState.FilesInspected) == 0 && len(a.planState.ActionsTaken) == 0 {
		b.WriteString("No files or actions recorded yet.\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (a *Agent) CompactContext() {
	a.compactContext()
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
	a.selectSkills(prompt)
	if err := a.persistSessionContext(ctx); err != nil {
		a.logError("failed to persist session skill/tool context: %v", err)
	}
	if len(a.messages) == 0 {
		selected := make([]string, 0, len(a.selectedSkills))
		for _, skill := range a.selectedSkills {
			selected = append(selected, skill.Name)
		}
		a.logDebug("selected skills=%v active_tools=%d prompt_bytes=%d", selected, len(a.activeTools), len(a.systemPrompt(prompt)))
		a.messages = append(a.messages, model.Message{Role: "system", Content: a.systemPrompt(prompt)})
		if a.sessionID != 0 {
			stored, err := a.store.ListMessages(ctx, a.sessionID)
			if err != nil {
				return "", err
			}
			for _, msg := range stored {
				loaded := model.Message{Role: msg.Role, Content: msg.Content}
				if msg.Metadata != "" {
					if err := json.Unmarshal([]byte(msg.Metadata), &loaded); err != nil {
						a.logError("failed to restore message metadata: %v", err)
					}
				}
				a.messages = append(a.messages, loaded)
			}
			toolCalls, err := a.store.ListToolCalls(ctx, a.sessionID)
			if err == nil && len(toolCalls) > 0 {
				var summary strings.Builder
				summary.WriteString("Tool call state from prior session:\n")
				for _, tc := range toolCalls {
					_, _ = fmt.Fprintf(&summary, "- %s: %s [%s]", tc.Name, tc.Arguments, tc.Status)
					if tc.Result != nil {
						if tc.Result.Error != "" {
							_, _ = fmt.Fprintf(&summary, " error=%s", tc.Result.Error)
						}
					}
					summary.WriteString("\n")
				}
				a.messages = append(a.messages, model.Message{Role: "system", Content: summary.String()})
			}
		}
	} else if len(a.messages) > 0 && a.messages[0].Role == "system" {
		// Skills and tool availability are session-turn specific. Refresh the
		// system context so a later request can move from, for example, Go to
		// Docker without retaining the first turn's allowlist.
		a.messages[0].Content = a.systemPrompt(prompt)
	}
	a.messages = append(a.messages, model.Message{Role: "user", Content: prompt})
	if err := a.store.AddMessage(ctx, a.sessionID, "user", prompt); err != nil {
		a.logError("failed to persist user message: %v", err)
	}

	useNative := a.cfg.Agent.ToolCalls == "native"

	for step := 0; step < a.maxSteps; step++ {
		a.currentStep = step + 1
		a.emit(Event{Type: "step_started", Summary: fmt.Sprintf("Agent step %d of %d", step+1, a.maxSteps)})
		if useNative {
			toolSchemas := a.tools.ToolSchemasFor(a.activeTools)
			res, err := a.chatWithTools(ctx, toolSchemas)
			if err != nil {
				return "", err
			}
			if len(res.ToolCalls) > 0 {
				a.logDebug("model returned %d native tool calls", len(res.ToolCalls))
				assistant := model.Message{Role: "assistant", Content: res.Content}
				toolResults := make([]model.Message, 0, len(res.ToolCalls))
				for _, tc := range res.ToolCalls {
					a.logDebug("  tool_call: %s args=%s", tc.Function.Name, string(tc.Function.Arguments))
					resultText, err := a.executeTool(ctx, toolCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
						ID:        tc.ID,
					})
					if err != nil {
						resultText = fmt.Sprintf(`{"ok":false,"summary":"tool failed","content":%q}`, err.Error())
					}
					assistant.ToolCalls = append(assistant.ToolCalls, model.ToolCall{
						ID:   tc.ID,
						Type: tc.Type,
						Function: model.ToolCallFunction{
							Name:      tc.Function.Name,
							Arguments: tc.Function.Arguments,
						},
					})
					toolResults = append(toolResults, model.Message{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content:    resultText,
					})
				}
				a.messages = append(a.messages, assistant)
				if err := a.store.AddMessage(ctx, a.sessionID, "assistant", res.Content, messageMetadata(assistant)); err != nil {
					a.logError("failed to persist assistant message: %v", err)
				}
				for _, toolResult := range toolResults {
					a.messages = append(a.messages, toolResult)
					if err := a.store.AddMessage(ctx, a.sessionID, "tool", toolResult.Content, messageMetadata(toolResult)); err != nil {
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
		if strings.TrimSpace(content) == "" {
			return "", fmt.Errorf("agent: model returned an empty response")
		}
		a.logDebug("model response: %s", debugTruncate(content, 500))
		call, ok, validationErr := parseToolCallDetailed(content)
		if validationErr != nil {
			a.logDebug("tool call validation error: %v", validationErr)
			a.messages = append(a.messages, model.Message{Role: "assistant", Content: content})
			correction := "Tool call validation error: " + validationErr.Error() + "\nRespond with exactly one valid tool_call JSON object or a final answer."
			a.messages = append(a.messages, model.Message{Role: "user", Content: correction})
			if err := a.store.AddMessage(ctx, a.sessionID, "assistant", content); err != nil {
				a.logError("failed to persist invalid assistant message: %v", err)
			}
			if err := a.store.AddMessage(ctx, a.sessionID, "user", correction); err != nil {
				a.logError("failed to persist tool validation message: %v", err)
			}
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
			if err := a.store.AddMessage(ctx, a.sessionID, "assistant", content); err != nil {
				a.logError("failed to persist assistant tool-call message: %v", err)
			}
			if err := a.store.AddMessage(ctx, a.sessionID, "tool", resultText); err != nil {
				a.logError("failed to persist tool result message: %v", err)
			}
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
	raw, err := json.Marshal(msg)
	if err != nil {
		return maxInt(1, len(msg.Content)/4)
	}
	return maxInt(1, len(raw)/4)
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
	prefixEnd := 0
	for prefixEnd < len(a.messages) && a.messages[prefixEnd].Role == "system" {
		prefixEnd++
	}
	systemMsgs := a.messages[:prefixEnd]
	rest := a.messages[prefixEnd:]
	compacted := model.Message{Role: "system", Content: "Previous conversation context was compacted to fit within the model's context window."}
	budget := threshold - a.messageTokens(systemMsgs) - a.estimateTokens(compacted)
	if budget < 1 {
		budget = 1
	}
	recent := make([]model.Message, 0, len(rest))
	used := 0
	for i := len(rest) - 1; i >= 0; i-- {
		cost := a.estimateTokens(rest[i])
		if len(recent) > 0 && used+cost > budget {
			break
		}
		msg := rest[i]
		if len(recent) == 0 && cost > budget {
			msg = fitMessageToBudget(a, msg, budget)
			cost = a.estimateTokens(msg)
		}
		recent = append([]model.Message{msg}, recent...)
		used += cost
	}
	recent = normalizeRecentMessages(recent)
	full := make([]model.Message, 0, len(systemMsgs)+len(recent)+1)
	full = append(full, systemMsgs...)
	full = append(full, compacted)
	full = append(full, recent...)
	a.messages = full
	a.emit(Event{Type: "context_compacted", Summary: "Conversation context was compacted to fit within the model's context window."})
}

func normalizeRecentMessages(messages []model.Message) []model.Message {
	for len(messages) > 0 {
		if messages[0].Role == "tool" {
			messages = messages[1:]
			continue
		}
		if messages[0].Role == "assistant" && len(messages[0].ToolCalls) > 0 {
			needed := make(map[string]bool, len(messages[0].ToolCalls))
			for _, call := range messages[0].ToolCalls {
				needed[call.ID] = true
			}
			for _, msg := range messages[1:] {
				if msg.Role == "tool" {
					delete(needed, msg.ToolCallID)
				}
			}
			if len(needed) > 0 {
				messages = messages[1:]
				continue
			}
		}
		break
	}
	return messages
}

func (a *Agent) messageTokens(messages []model.Message) int {
	total := 0
	for _, msg := range messages {
		total += a.estimateTokens(msg)
	}
	return total
}

func truncateMessage(msg model.Message, maxChars int) model.Message {
	if maxChars < 1 {
		maxChars = 1
	}
	if len(msg.Content) <= maxChars {
		return msg
	}
	const marker = "\n[context truncated]"
	if maxChars <= len(marker) {
		msg.Content = ""
	} else {
		msg.Content = msg.Content[:maxChars-len(marker)] + marker
	}
	return msg
}

func fitMessageToBudget(a *Agent, msg model.Message, budget int) model.Message {
	for {
		cost := a.estimateTokens(msg)
		if cost <= budget || len(msg.Content) <= 1 {
			return msg
		}
		maxChars := len(msg.Content) * budget / cost
		if maxChars >= len(msg.Content) {
			maxChars = len(msg.Content) - 1
		}
		msg = truncateMessage(msg, maxChars)
	}
}

func (a *Agent) chat(ctx context.Context) (string, error) {
	a.compactContext()
	if !a.streaming || a.streamCallback == nil {
		var result string
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			result, err = a.client.Chat(ctx, a.messages, a.cfg.Runtime.Temperature, a.cfg.Runtime.TopP)
			if err == nil || !retryableModelError(ctx, err) || attempt == 2 {
				return result, err
			}
			if err := waitModelRetry(ctx, attempt, err); err != nil {
				return "", err
			}
		}
		return result, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		ch, err := a.client.ChatStream(ctx, a.messages, a.cfg.Runtime.Temperature, a.cfg.Runtime.TopP)
		if err != nil {
			if !retryableModelError(ctx, err) || attempt == 2 {
				return "", err
			}
			if err := waitModelRetry(ctx, attempt, err); err != nil {
				return "", err
			}
			continue
		}
		var full strings.Builder
		for chunk := range ch {
			if chunk.Err != nil {
				return full.String(), chunk.Err
			}
			full.WriteString(chunk.Content)
			a.streamCallback(chunk.Content)
		}
		if strings.TrimSpace(full.String()) == "" {
			// Some OpenAI-compatible providers advertise streaming but close an
			// empty SSE response for particular hosted models. Retry once through
			// the non-streaming endpoint before reporting success.
			fallback, fallbackErr := a.client.Chat(ctx, a.messages, a.cfg.Runtime.Temperature, a.cfg.Runtime.TopP)
			if fallbackErr != nil {
				return "", fallbackErr
			}
			return fallback, nil
		}
		return full.String(), nil
	}
	return "", ctx.Err()
}

func (a *Agent) chatWithTools(ctx context.Context, tools []model.ToolSchema) (*model.ResponseMessage, error) {
	a.compactContext()
	var result *model.ResponseMessage
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		result, err = a.client.ChatWithTools(ctx, a.messages, a.cfg.Runtime.Temperature, a.cfg.Runtime.TopP, tools)
		if err == nil || !retryableModelError(ctx, err) || attempt == 2 {
			return result, err
		}
		if err := waitModelRetry(ctx, attempt, err); err != nil {
			return nil, err
		}
	}
	return result, err
}

func retryableModelError(ctx context.Context, err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{"status 408", "status 409", "status 425", "status 429", "status 500", "status 502", "status 503", "status 504", "connection refused", "connection reset", "timeout", "temporarily unavailable", "eof"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func waitModelRetry(ctx context.Context, attempt int, modelErr error) error {
	delay := time.Duration(1<<attempt) * 500 * time.Millisecond
	if retryAfter := model.RetryAfter(modelErr); retryAfter > 0 {
		if retryAfter > delay {
			delay = retryAfter
		}
	} else if delay > 0 {
		// Jitter prevents multiple clients recovering from a shared rate limit
		// at the same instant.
		delay = delay/2 + time.Duration(rand.Int64N(int64(delay/2)+1))
	}
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (a *Agent) selectSkills(userPrompt string) {
	var toolDescriptors []tools.ToolDescriptor
	if a.tools != nil {
		toolDescriptors = a.tools.Descriptors()
	}
	mcpDescriptors := make([]skills.ToolDescriptor, 0, len(toolDescriptors))
	for _, descriptor := range toolDescriptors {
		if strings.HasPrefix(descriptor.Name, "mcp_") {
			mcpDescriptors = append(mcpDescriptors, skills.ToolDescriptor{Name: descriptor.Name, Description: descriptor.Description})
		}
	}
	selection := skills.SelectWithTools(a.skills, mcpDescriptors, userPrompt)
	a.selectedSkills = selection.Skills
	a.activeTools = selection.ActiveTools
	// Keep allowedTools as the legacy skill-pack view for callers that used
	// it diagnostically; execution and provider exposure use activeTools.
	a.allowedTools = nil
	for _, selected := range selection.Skills {
		a.allowedTools = append(a.allowedTools, selected.Meta.AllowedTools...)
	}
	sort.Strings(a.allowedTools)
	a.selectionMatches = selection.Matches
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
	if rendered := skills.RenderInstructions(a.instructions, 12000); rendered != "" {
		b.WriteString(rendered)
		b.WriteString("\n")
	}
	toolPrompt := a.tools.PromptFor(a.activeTools)
	b.WriteString(toolPrompt)
	if rendered := skills.RenderSliced(a.selectedSkills, userPrompt, 8000); rendered != "" {
		b.WriteString("\n")
		b.WriteString(rendered)
	}
	if scripts := skills.Scripts(a.selectedSkills); len(scripts) > 0 {
		b.WriteString("\nSkill-defined scripts — use run_script with the description when appropriate; shell approval policy still applies:\n")
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

type toolCallEnvelope struct {
	ToolCall toolCall `json:"tool_call"`
}

type toolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	ID        string          `json:"id,omitempty"`
}

func (a *Agent) toolContextJSON(nativeID string) string {
	names := make([]string, 0, len(a.selectedSkills))
	for _, skill := range a.selectedSkills {
		names = append(names, skill.Name)
	}
	data, err := json.Marshal(map[string]interface{}{
		"native_id":     nativeID,
		"skills":        names,
		"allowed_tools": a.activeTools,
		"tool_mode":     a.cfg.Agent.ToolCalls,
		"skill_matches": a.selectionMatches,
	})
	if err != nil {
		return ""
	}
	return string(data)
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
		if err := normalizeToolArguments(&env.ToolCall); err != nil {
			return toolCall{}, false, fmt.Errorf("invalid tool_call arguments: %w", err)
		}
		return env.ToolCall, true, nil
	} else if looksLikeToolCall(content) && err != nil {
		return toolCall{}, false, fmt.Errorf("invalid tool_call JSON: %w", err)
	}

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(content[start:end+1]), &env); err == nil && env.ToolCall.Name != "" {
			if err := normalizeToolArguments(&env.ToolCall); err != nil {
				return toolCall{}, false, fmt.Errorf("invalid embedded tool_call arguments: %w", err)
			}
			return env.ToolCall, true, nil
		} else if looksLikeToolCall(content[start : end+1]) {
			return toolCall{}, false, fmt.Errorf("invalid embedded tool_call JSON: %w", err)
		}
	}
	return toolCall{}, false, nil
}

func normalizeToolArguments(call *toolCall) error {
	var encoded string
	if err := json.Unmarshal(call.Arguments, &encoded); err != nil {
		return nil
	}
	decoded := json.RawMessage(encoded)
	if !json.Valid(decoded) {
		return fmt.Errorf("arguments string is not valid JSON")
	}
	call.Arguments = decoded
	return nil
}

func looksLikeToolCall(content string) bool {
	return strings.Contains(content, "tool_call") || strings.Contains(content, `"arguments"`) && strings.Contains(content, `"name"`)
}

func (a *Agent) executeTool(ctx context.Context, call toolCall) (string, error) {
	if a.activeTools != nil {
		allowed := false
		for _, name := range a.activeTools {
			if call.Name == name {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", fmt.Errorf("tool %q is not allowed by active skills", call.Name)
		}
	}
	if call.Name == "run_script" {
		return a.executeScript(ctx, call)
	}

	tool, ok := a.tools.Get(call.Name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", call.Name)
	}
	a.logDebug("execute tool: %s args=%s", call.Name, string(call.Arguments))
	callID, err := a.store.AddToolCall(ctx, a.sessionID, call.Name, string(call.Arguments), "requested", a.toolContextJSON(call.ID))
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
	approved, denied := a.approvalPolicy(call.Name, effect, call.Arguments)
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
			request := ApprovalRequest{Tool: call.Name, Kind: effect, Summary: summary, Diff: diff}
			decision := ApprovalDeny
			if advanced, ok := a.approver.(AdvancedApprover); ok {
				decision = advanced.ApproveWithOptions(request)
			} else if a.approver != nil {
				if a.approver.Approve(request) {
					decision = ApprovalOnce
				}
			}
			if decision == ApprovalDeny {
				a.emit(Event{Type: "approval_denied", Tool: call.Name, Effect: effect, Summary: summary})
				if err := a.store.AddToolResult(ctx, callID, "", "approval denied"); err != nil {
					a.logError("failed to persist tool result: %v", err)
				}
				if err := a.store.AddApproval(ctx, a.sessionID, callID, call.Name, effect, summary, false); err != nil {
					a.logError("failed to persist approval: %v", err)
				}
				return `{"ok":false,"summary":"approval denied"}`, nil
			}
			approvalType := "once"
			switch decision {
			case ApprovalSession:
				approvalType = "for session"
			case ApprovalAlways:
				approvalType = "always for tool"
			}
			a.emit(Event{Type: "approval_approved", Tool: call.Name, Effect: effect, Summary: summary, Detail: approvalType})
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
	needle := strings.TrimSpace(args.Description)
	exactMatches := 0
	for _, skill := range a.selectedSkills {
		for _, script := range skill.Meta.Scripts {
			if strings.EqualFold(strings.TrimSpace(script.Description), needle) {
				exactMatches++
				if exactMatches == 1 {
					s := script
					found = &s
					skillName = skill.Name
				}
			}
		}
	}
	if exactMatches > 1 {
		return "", fmt.Errorf("script description %q is ambiguous; duplicate exact descriptions exist", args.Description)
	}
	if found == nil {
		// Preserve convenient partial matching, but only accept a unique best
		// match. A vague request must never silently run an arbitrary script.
		bestScore := 0
		matches := 0
		for _, skill := range a.selectedSkills {
			for _, script := range skill.Meta.Scripts {
				desc := strings.ToLower(script.Description)
				cmd := strings.ToLower(script.Command)
				query := strings.ToLower(needle)
				score := 0
				if strings.Contains(desc, query) {
					score = 2
				} else if strings.Contains(cmd, query) {
					score = 1
				}
				if score == 0 {
					continue
				}
				if score > bestScore {
					bestScore, matches = score, 0
					found = nil
				}
				if score == bestScore {
					matches++
					if matches == 1 {
						s := script
						found = &s
						skillName = skill.Name
					}
				}
			}
		}
		if matches > 1 {
			return "", fmt.Errorf("script description %q is ambiguous; use an exact description", args.Description)
		}
	}
	if found == nil {
		return "", fmt.Errorf("no script matching %q found in active skills", args.Description)
	}
	scriptTool := strings.TrimSpace(found.Tool)
	if scriptTool == "" {
		scriptTool = "run_command"
	}
	if scriptTool != "run_command" {
		return "", fmt.Errorf("script %q declares unsupported tool %q; only run_command is supported", found.Description, scriptTool)
	}

	callID, err := a.store.AddToolCall(ctx, a.sessionID, "run_script", string(call.Arguments), "requested", a.toolContextJSON(call.ID))
	if err != nil {
		a.logError("failed to persist tool call: %v", err)
	}

	effect := "shell"
	summary := fmt.Sprintf("run_script %q (from skill %q)", found.Description, skillName)
	a.emit(Event{Type: "tool_requested", Tool: "run_script", Effect: effect, Summary: summary})
	approved, denied := a.approvalPolicy("run_script", effect, call.Arguments)
	if denied {
		a.emit(Event{Type: "approval_denied", Tool: "run_script", Effect: effect, Summary: summary})
		_ = a.store.AddToolResult(ctx, callID, "", "denied by policy")
		_ = a.store.AddApproval(ctx, a.sessionID, callID, "run_script", effect, summary, false)
		return `{"ok":false,"summary":"approval denied by policy"}`, nil
	}
	if !approved {
		a.emit(Event{Type: "approval_requested", Tool: "run_script", Effect: effect, Summary: summary, Detail: found.Command})
		decision := ApprovalDeny
		if advanced, ok := a.approver.(AdvancedApprover); ok {
			decision = advanced.ApproveWithOptions(ApprovalRequest{Tool: "run_script", Kind: effect, Summary: summary, Diff: found.Command})
		} else if a.approver != nil && a.approver.Approve(ApprovalRequest{Tool: "run_script", Kind: effect, Summary: summary, Diff: found.Command}) {
			decision = ApprovalOnce
		}
		if decision == ApprovalDeny {
			a.emit(Event{Type: "approval_denied", Tool: "run_script", Effect: effect, Summary: summary})
			_ = a.store.AddToolResult(ctx, callID, "", "approval denied")
			_ = a.store.AddApproval(ctx, a.sessionID, callID, "run_script", effect, summary, false)
			return `{"ok":false,"summary":"approval denied"}`, nil
		}
	}
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
			"script":     found.Description,
			"skill":      skillName,
			"command":    found.Command,
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
		_, _ = fmt.Fprintf(a.debugWriter, "%s ERROR %s\n", ts, msg)
	}
}

func (a *Agent) logDebug(format string, args ...interface{}) {
	if a.debugWriter == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().UTC().Format(time.RFC3339)
	_, _ = fmt.Fprintf(a.debugWriter, "%s DEBUG %s\n", ts, msg)
}

func (a *Agent) emit(event Event) {
	if a.observer != nil {
		a.observer.OnEvent(event)
	}
}

func (a *Agent) approvalPolicy(toolName, effect string, raw json.RawMessage) (approve bool, deny bool) {
	cfg := a.cfg.Approval
	if policy, ok := a.mcpToolPolicies[toolName]; ok {
		return policyFromConfig(policy)
	}
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

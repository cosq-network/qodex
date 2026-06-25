package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/benoybose/locha/internal/config"
	"github.com/benoybose/locha/internal/model"
	"github.com/benoybose/locha/internal/skills"
	"github.com/benoybose/locha/internal/store"
	"github.com/benoybose/locha/internal/tools"
)

type Options struct {
	Config    config.Config
	Client    *model.Client
	Tools     *tools.Registry
	Store     *store.Store
	Skills    []skills.Skill
	Approver  Approver
	Observer  Observer
	MaxSteps  int
	SessionID int64
}

type Agent struct {
	cfg       config.Config
	client    *model.Client
	tools     *tools.Registry
	store     *store.Store
	skills    []skills.Skill
	approver  Approver
	observer  Observer
	maxSteps  int
	sessionID int64
	messages  []model.Message
}

type ApprovalRequest struct {
	Kind    string
	Summary string
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
		cfg:       opts.Config,
		client:    opts.Client,
		tools:     opts.Tools,
		store:     opts.Store,
		skills:    opts.Skills,
		approver:  opts.Approver,
		observer:  opts.Observer,
		maxSteps:  maxSteps,
		sessionID: opts.SessionID,
	}
}

func (a *Agent) SetApprover(approver Approver) {
	a.approver = approver
}

func (a *Agent) SetObserver(observer Observer) {
	a.observer = observer
}

func (a *Agent) Run(ctx context.Context, prompt string) (string, error) {
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
		a.messages = append(a.messages, model.Message{Role: "system", Content: a.systemPrompt(prompt)})
		if a.sessionID != 0 {
			stored, err := a.store.ListMessages(ctx, a.sessionID)
			if err != nil {
				return "", err
			}
			for _, msg := range stored {
				a.messages = append(a.messages, model.Message{Role: msg.Role, Content: msg.Content})
			}
		}
	}
	a.messages = append(a.messages, model.Message{Role: "user", Content: prompt})
	_ = a.store.AddMessage(ctx, a.sessionID, "user", prompt)

	for step := 0; step < a.maxSteps; step++ {
		content, err := a.client.Chat(ctx, a.messages, a.cfg.Runtime.Temperature, a.cfg.Runtime.TopP)
		if err != nil {
			return "", err
		}
		call, ok, validationErr := parseToolCallDetailed(content)
		if validationErr != nil {
			a.messages = append(a.messages, model.Message{Role: "assistant", Content: content})
			a.messages = append(a.messages, model.Message{Role: "user", Content: "Tool call validation error: " + validationErr.Error() + "\nRespond with exactly one valid tool_call JSON object or a final answer."})
			continue
		}
		if ok {
			resultText, err := a.executeTool(ctx, call)
			if err != nil {
				resultText = fmt.Sprintf(`{"ok":false,"summary":"tool failed","content":%q}`, err.Error())
			}
			a.messages = append(a.messages, model.Message{Role: "assistant", Content: content})
			a.messages = append(a.messages, model.Message{Role: "user", Content: "Tool result:\n" + resultText})
			continue
		}
		a.messages = append(a.messages, model.Message{Role: "assistant", Content: content})
		_ = a.store.AddMessage(ctx, a.sessionID, "assistant", content)
		return content, nil
	}
	return "", fmt.Errorf("agent stopped after %d steps", a.maxSteps)
}

func (a *Agent) systemPrompt(userPrompt string) string {
	selected := skills.Select(a.skills, userPrompt)
	var b strings.Builder
	b.WriteString("You are Locha, a local coding agent running on the user's machine.\n")
	b.WriteString("You must not claim to have read, changed, or executed anything unless a tool result proves it.\n")
	b.WriteString("When you need a tool, respond with exactly one JSON object and no Markdown:\n")
	b.WriteString(`{"tool_call":{"name":"read_file","arguments":{"path":"README.md"}}}` + "\n")
	b.WriteString("When you have enough information, respond normally with the final answer.\n")
	b.WriteString("Prefer narrow reads and searches before edits. Explain risky actions before requesting shell commands.\n\n")
	b.WriteString(a.tools.Prompt())
	if rendered := skills.Render(selected, 8000); rendered != "" {
		b.WriteString("\n")
		b.WriteString(rendered)
	}
	return b.String()
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
	tool, ok := a.tools.Get(call.Name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", call.Name)
	}
	callID, _ := a.store.AddToolCall(ctx, a.sessionID, call.Name, string(call.Arguments), "requested")

	effect := tool.Effect
	if call.Name == "run_command" && tools.IsNetworkCommand(call.Arguments) {
		effect = "network"
	}
	summary := call.Name + " " + string(call.Arguments)
	a.emit(Event{Type: "tool_requested", Tool: call.Name, Effect: effect, Summary: summary})
	if effect == "write" || effect == "shell" || effect == "network" || effect == "destructive" {
		a.emit(Event{Type: "approval_requested", Tool: call.Name, Effect: effect, Summary: summary})
		approved := a.approver != nil && a.approver.Approve(ApprovalRequest{Kind: effect, Summary: summary})
		if !approved {
			a.emit(Event{Type: "approval_denied", Tool: call.Name, Effect: effect, Summary: summary})
			_ = a.store.AddToolResult(ctx, callID, "", "approval denied")
			return `{"ok":false,"summary":"approval denied"}`, nil
		}
		a.emit(Event{Type: "approval_approved", Tool: call.Name, Effect: effect, Summary: summary})
	}

	res, err := tool.Execute(ctx, call.Arguments)
	raw, _ := json.Marshal(res)
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	_ = a.store.AddToolResult(ctx, callID, string(raw), errText)
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

func (a *Agent) emit(event Event) {
	if a.observer != nil {
		a.observer.OnEvent(event)
	}
}

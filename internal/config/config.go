package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	ProjectRoot string
	Model       ModelConfig
	Runtime     RuntimeConfig
	Approval    ApprovalConfig
	Store       StoreConfig
	Agent       AgentConfig
	MCP         MCPConfig
}

type ModelConfig struct {
	Provider string
	BaseURL  string
	Model    string
}

type RuntimeConfig struct {
	Backend       string
	ContextTokens int
	Temperature   float64
	TopP          float64
}

type ApprovalConfig struct {
	AutoApprove bool
	WriteFiles  string
	RunCommands string
	Network     string
}

type StoreConfig struct {
	Path string
}

type AgentConfig struct {
	MaxSteps     int
	SkillRouting string
	ToolCalls    string // "prompt" (default) or "native"
}

type MCPConfig struct {
	Servers map[string]MCPServerConfig
}

type MCPServerConfig struct {
	Transport   string
	Command     string
	Args        []string
	URL         string
	Headers     map[string]string
	Env         map[string]string
	Auth        MCPAuthConfig
	Trust       string
	Permissions map[string]string
	Enabled     bool
}

// MCPAuthConfig describes how a stdio server receives a credential without
// storing the secret in Qodex configuration. TokenEnv is read from Qodex's
// environment and copied to PassEnv for the child process.
type MCPAuthConfig struct {
	Type     string
	TokenEnv string
	PassEnv  string
	Header   string
}

func Load(path string) (Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Config{}, err
	}
	cfg := Defaults(cwd)
	paths := candidatePaths(cwd, path)
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			if err := mergeFile(&cfg, p); err != nil {
				return Config{}, err
			}
		}
	}
	normalize(&cfg)
	if err := os.MkdirAll(filepath.Dir(cfg.Store.Path), 0o755); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Defaults(projectRoot string) Config {
	storePath := filepath.Join(projectRoot, ".qodex", "qodex.db")
	return Config{
		ProjectRoot: projectRoot,
		Model: ModelConfig{
			Provider: "openai-compatible",
			BaseURL:  "http://127.0.0.1:8080/v1",
			Model:    "qwen2.5-coder",
		},
		Runtime: RuntimeConfig{
			Backend:       "llama.cpp",
			ContextTokens: 32768,
			Temperature:   0.2,
			TopP:          0.95,
		},
		Approval: ApprovalConfig{
			WriteFiles:  "ask",
			RunCommands: "ask",
			Network:     "ask",
		},
		Store: StoreConfig{Path: storePath},
		Agent: AgentConfig{MaxSteps: 12, SkillRouting: "auto", ToolCalls: "prompt"},
		MCP:   MCPConfig{Servers: map[string]MCPServerConfig{}},
	}
}

func candidatePaths(cwd, explicit string) []string {
	if explicit != "" {
		return []string{explicit}
	}
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".config", "qodex", "config.toml"),
		filepath.Join(cwd, ".qodex", "config.toml"),
	}
}

func mergeFile(cfg *Config, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	var next fileConfig
	if err := toml.NewDecoder(file).DisallowUnknownFields().Decode(&next); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	next.mergeInto(cfg)
	return nil
}

type fileConfig struct {
	Model    fileModelConfig    `toml:"model"`
	Runtime  fileRuntimeConfig  `toml:"runtime"`
	Approval fileApprovalConfig `toml:"approval"`
	Store    fileStoreConfig    `toml:"store"`
	Agent    fileAgentConfig    `toml:"agent"`
	MCP      fileMCPConfig      `toml:"mcp"`
}

type fileModelConfig struct {
	Provider *string `toml:"provider"`
	BaseURL  *string `toml:"base_url"`
	Model    *string `toml:"model"`
}

type fileRuntimeConfig struct {
	Backend       *string  `toml:"backend"`
	ContextTokens *int     `toml:"context_tokens"`
	Temperature   *float64 `toml:"temperature"`
	TopP          *float64 `toml:"top_p"`
}

type fileApprovalConfig struct {
	AutoApprove *bool   `toml:"auto_approve"`
	WriteFiles  *string `toml:"write_files"`
	RunCommands *string `toml:"run_commands"`
	Network     *string `toml:"network"`
}

type fileStoreConfig struct {
	Path *string `toml:"path"`
}

type fileAgentConfig struct {
	MaxSteps     *int    `toml:"max_steps"`
	SkillRouting *string `toml:"skill_routing"`
	ToolCalls    *string `toml:"tool_calls"`
}

type fileMCPConfig struct {
	Servers map[string]fileMCPServerConfig `toml:"servers"`
}

type fileMCPServerConfig struct {
	Transport   string            `toml:"transport"`
	Command     string            `toml:"command"`
	Args        []string          `toml:"args"`
	URL         string            `toml:"url"`
	Headers     map[string]string `toml:"headers"`
	Env         map[string]string `toml:"env"`
	Auth        fileMCPAuthConfig `toml:"auth"`
	Trust       string            `toml:"trust"`
	Permissions map[string]string `toml:"permissions"`
	Enabled     *bool             `toml:"enabled"`
}

type fileMCPAuthConfig struct {
	Type     string `toml:"type"`
	TokenEnv string `toml:"token_env"`
	PassEnv  string `toml:"pass_env"`
	Header   string `toml:"header"`
}

func (f fileConfig) mergeInto(cfg *Config) {
	if f.Model.Provider != nil {
		cfg.Model.Provider = *f.Model.Provider
	}
	if f.Model.BaseURL != nil {
		cfg.Model.BaseURL = strings.TrimRight(*f.Model.BaseURL, "/")
	}
	if f.Model.Model != nil {
		cfg.Model.Model = *f.Model.Model
	}
	if f.Runtime.Backend != nil {
		cfg.Runtime.Backend = *f.Runtime.Backend
	}
	if f.Runtime.ContextTokens != nil {
		cfg.Runtime.ContextTokens = *f.Runtime.ContextTokens
	}
	if f.Runtime.Temperature != nil {
		cfg.Runtime.Temperature = *f.Runtime.Temperature
	}
	if f.Runtime.TopP != nil {
		cfg.Runtime.TopP = *f.Runtime.TopP
	}
	if f.Approval.AutoApprove != nil {
		cfg.Approval.AutoApprove = *f.Approval.AutoApprove
	}
	if f.Approval.WriteFiles != nil {
		cfg.Approval.WriteFiles = *f.Approval.WriteFiles
	}
	if f.Approval.RunCommands != nil {
		cfg.Approval.RunCommands = *f.Approval.RunCommands
	}
	if f.Approval.Network != nil {
		cfg.Approval.Network = *f.Approval.Network
	}
	if f.Store.Path != nil {
		cfg.Store.Path = *f.Store.Path
	}
	if f.Agent.MaxSteps != nil {
		cfg.Agent.MaxSteps = *f.Agent.MaxSteps
	}
	if f.Agent.SkillRouting != nil {
		cfg.Agent.SkillRouting = *f.Agent.SkillRouting
	}
	if f.Agent.ToolCalls != nil {
		cfg.Agent.ToolCalls = *f.Agent.ToolCalls
	}
	if cfg.MCP.Servers == nil {
		cfg.MCP.Servers = map[string]MCPServerConfig{}
	}
	for name, server := range f.MCP.Servers {
		enabled := true
		if server.Enabled != nil {
			enabled = *server.Enabled
		}
		cfg.MCP.Servers[name] = MCPServerConfig{
			Transport: server.Transport, Command: server.Command, Args: server.Args, URL: server.URL,
			Headers: server.Headers, Env: server.Env,
			Auth:  MCPAuthConfig{Type: server.Auth.Type, TokenEnv: server.Auth.TokenEnv, PassEnv: server.Auth.PassEnv, Header: server.Auth.Header},
			Trust: server.Trust, Permissions: server.Permissions, Enabled: enabled,
		}
	}
}

func apply(cfg *Config, section, key, val string) {
	switch section + "." + key {
	case "model.provider":
		cfg.Model.Provider = val
	case "model.base_url":
		cfg.Model.BaseURL = strings.TrimRight(val, "/")
	case "model.model":
		cfg.Model.Model = val
	case "runtime.backend":
		cfg.Runtime.Backend = val
	case "runtime.context_tokens":
		cfg.Runtime.ContextTokens = intVal(val, cfg.Runtime.ContextTokens)
	case "runtime.temperature":
		cfg.Runtime.Temperature = floatVal(val, cfg.Runtime.Temperature)
	case "runtime.top_p":
		cfg.Runtime.TopP = floatVal(val, cfg.Runtime.TopP)
	case "approval.auto_approve":
		cfg.Approval.AutoApprove = boolVal(val)
	case "approval.write_files":
		cfg.Approval.WriteFiles = val
	case "approval.run_commands":
		cfg.Approval.RunCommands = val
	case "approval.network":
		cfg.Approval.Network = val
	case "store.path":
		cfg.Store.Path = val
	case "agent.max_steps":
		cfg.Agent.MaxSteps = intVal(val, cfg.Agent.MaxSteps)
	case "agent.skill_routing":
		cfg.Agent.SkillRouting = val
	case "agent.tool_calls":
		cfg.Agent.ToolCalls = val
	}
}

func normalize(cfg *Config) {
	cfg.Model.BaseURL = strings.TrimRight(cfg.Model.BaseURL, "/")
	cfg.Store.Path = expandHome(cfg.Store.Path)
	if cfg.Store.Path != "" && !filepath.IsAbs(cfg.Store.Path) {
		cfg.Store.Path = filepath.Join(cfg.ProjectRoot, cfg.Store.Path)
	}
}

func intVal(s string, fallback int) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}

func floatVal(s string, fallback float64) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fallback
	}
	return v
}

func boolVal(s string) bool {
	return s == "true" || s == "1" || s == "yes"
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return path
}

func (c Config) Validate() error {
	if c.Model.BaseURL == "" {
		return fmt.Errorf("model.base_url is required")
	}
	if _, err := url.ParseRequestURI(c.Model.BaseURL); err != nil {
		return fmt.Errorf("model.base_url is invalid: %w", err)
	}
	if c.Model.Provider != "openai-compatible" {
		return fmt.Errorf("model.provider must be openai-compatible")
	}
	if c.Model.Model == "" {
		return fmt.Errorf("model.model is required")
	}
	switch c.Runtime.Backend {
	case "llama.cpp", "vllm", "sglang", "external":
	default:
		return fmt.Errorf("runtime.backend must be one of: llama.cpp, vllm, sglang, external")
	}
	if c.Runtime.ContextTokens <= 0 {
		return fmt.Errorf("runtime.context_tokens must be positive")
	}
	if c.Runtime.Temperature < 0 || c.Runtime.Temperature > 2 {
		return fmt.Errorf("runtime.temperature must be between 0 and 2")
	}
	if c.Runtime.TopP <= 0 || c.Runtime.TopP > 1 {
		return fmt.Errorf("runtime.top_p must be greater than 0 and at most 1")
	}
	if c.Agent.MaxSteps <= 0 {
		return fmt.Errorf("agent.max_steps must be positive")
	}
	if c.Agent.SkillRouting != "auto" && c.Agent.SkillRouting != "model" {
		return fmt.Errorf("agent.skill_routing must be auto or model")
	}
	if c.Agent.ToolCalls != "prompt" && c.Agent.ToolCalls != "native" {
		return fmt.Errorf("agent.tool_calls must be prompt or native")
	}
	for name, server := range c.MCP.Servers {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("mcp server names are required")
		}
		transport := server.Transport
		if transport == "" {
			transport = "stdio"
		}
		if transport != "stdio" && transport != "streamable-http" {
			return fmt.Errorf("mcp.servers.%s.transport must be stdio or streamable-http", name)
		}
		if transport == "stdio" && strings.TrimSpace(server.Command) == "" {
			return fmt.Errorf("mcp server %s requires a command for stdio transport", name)
		}
		if transport == "streamable-http" {
			parsed, err := url.ParseRequestURI(server.URL)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
				return fmt.Errorf("mcp.servers.%s.url must be an absolute http(s) URL", name)
			}
		}
		if server.Trust != "" && server.Trust != "ask" && server.Trust != "trusted" && server.Trust != "blocked" {
			return fmt.Errorf("mcp.servers.%s.trust must be ask, trusted, or blocked", name)
		}
		if server.Auth.Type != "" && server.Auth.Type != "none" && server.Auth.Type != "bearer" && server.Auth.Type != "api_key" && server.Auth.Type != "oauth" {
			return fmt.Errorf("mcp.servers.%s.auth.type must be none, bearer, api_key, or oauth", name)
		}
		if server.Auth.Type != "" && server.Auth.Type != "none" && strings.TrimSpace(server.Auth.TokenEnv) == "" {
			return fmt.Errorf("mcp.servers.%s.auth.token_env is required when authentication is enabled", name)
		}
		for tool, policy := range server.Permissions {
			if policy != "ask" && policy != "allow" && policy != "deny" {
				return fmt.Errorf("mcp.servers.%s.permissions.%s must be ask, allow, or deny", name, tool)
			}
		}
		for field, value := range map[string]string{"token_env": server.Auth.TokenEnv, "pass_env": server.Auth.PassEnv} {
			if strings.ContainsAny(value, "=\r\n") {
				return fmt.Errorf("mcp.servers.%s.auth.%s is not a valid environment variable name", name, field)
			}
		}
	}
	for key, value := range map[string]string{
		"approval.write_files":  c.Approval.WriteFiles,
		"approval.run_commands": c.Approval.RunCommands,
		"approval.network":      c.Approval.Network,
	} {
		if value != "ask" && value != "allow" && value != "deny" {
			return fmt.Errorf("%s must be ask, allow, or deny", key)
		}
	}
	return nil
}

func (c Config) Values() map[string]string {
	return map[string]string{
		"model.provider":         c.Model.Provider,
		"model.base_url":         c.Model.BaseURL,
		"model.model":            c.Model.Model,
		"runtime.backend":        c.Runtime.Backend,
		"runtime.context_tokens": strconv.Itoa(c.Runtime.ContextTokens),
		"runtime.temperature":    strconv.FormatFloat(c.Runtime.Temperature, 'f', -1, 64),
		"runtime.top_p":          strconv.FormatFloat(c.Runtime.TopP, 'f', -1, 64),
		"approval.auto_approve":  strconv.FormatBool(c.Approval.AutoApprove),
		"approval.write_files":   c.Approval.WriteFiles,
		"approval.run_commands":  c.Approval.RunCommands,
		"approval.network":       c.Approval.Network,
		"store.path":             c.Store.Path,
		"agent.max_steps":        strconv.Itoa(c.Agent.MaxSteps),
		"agent.skill_routing":    c.Agent.SkillRouting,
		"agent.tool_calls":       c.Agent.ToolCalls,
		"mcp.servers":            strconv.Itoa(len(c.MCP.Servers)),
	}
}

func (c Config) Get(key string) (string, bool) {
	v, ok := c.Values()[key]
	return v, ok
}

func ProjectConfigPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".qodex", "config.toml")
}

func SetProjectValue(projectRoot, key, value string) error {
	if _, ok := Defaults(projectRoot).Get(key); !ok {
		return fmt.Errorf("unknown config key: %s", key)
	}
	cfg := Defaults(projectRoot)
	apply(&cfg, strings.TrimSuffix(sectionOf(key), "."), nameOf(key), value)
	normalize(&cfg)
	if err := cfg.Validate(); err != nil {
		return err
	}

	path := ProjectConfigPath(projectRoot)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	updated := setLine(string(data), key, value)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(updated), 0o666)
}

func sectionOf(key string) string {
	section, _, _ := strings.Cut(key, ".")
	return section
}

func nameOf(key string) string {
	_, name, ok := strings.Cut(key, ".")
	if !ok {
		return key
	}
	return name
}

func setLine(content, dottedKey, value string) string {
	section := sectionOf(dottedKey)
	name := nameOf(dottedKey)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	current := ""
	insertAt := len(lines)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if current == section {
				insertAt = i
			}
			current = strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
			continue
		}
		if current == section {
			if strings.HasPrefix(trimmed, name+" ") || strings.HasPrefix(trimmed, name+"=") {
				lines[i] = name + " = " + formatValue(value)
				return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
			}
		}
	}
	if insertAt == len(lines) {
		if strings.TrimSpace(content) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "["+section+"]", name+" = "+formatValue(value))
	} else {
		next := append([]string{name + " = " + formatValue(value)}, lines[insertAt:]...)
		lines = append(lines[:insertAt], next...)
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

func formatValue(value string) string {
	if value == "true" || value == "false" {
		return value
	}
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return value
	}
	return strconv.Quote(value)
}

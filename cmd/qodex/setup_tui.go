package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/benoybose/qodex/internal/config"
	"github.com/benoybose/qodex/internal/model"
)

type setupWizardStep int

const (
	setupWizardChoose setupWizardStep = iota
	setupWizardCatalog
	setupWizardConfirm
	setupWizardCredential
	setupWizardHostedModels
)

type setupHostedModelsMsg struct {
	models []string
	info   []model.HostedModelInfo
	err    error
}

type setupWizardModel struct {
	profile      setupProfile
	backend      model.Backend
	defaultModel string
	registry     setupModelSource
	step         setupWizardStep
	choice       int
	modelChoice  int
	catalog      []model.ModelInfo
	hosted       []string
	hostedInfo   []model.HostedModelInfo
	provider     string
	baseURL      string
	defaultHost  string
	tokenEnv     string
	apiKey       string
	credential   textinput.Model
	notice       string
	target       setupTarget
	done         bool
	err          error
}

var (
	setupTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	setupMutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	setupChoiceStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	setupErrorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

func runSetupTUI(projectRoot string) error {
	profile := detectSetupProfile()
	backend := automaticSetupBackend()
	defaultModel := setupModelForProfile(profile)
	wizard := newSetupWizardModel(profile, backend, defaultModel, model.NewModelRegistry(getInstallRoot()))
	program := tea.NewProgram(wizard, tea.WithAltScreen())
	final, err := program.Run()
	if err != nil {
		return err
	}
	result := final.(setupWizardModel)
	if result.err != nil {
		return result.err
	}
	if !result.done {
		return fmt.Errorf("setup cancelled")
	}
	return continueSetup(projectRoot, profile, backend, defaultModel, result.target)
}

func newSetupWizardModel(profile setupProfile, backend model.Backend, defaultModel string, registry setupModelSource) setupWizardModel {
	return setupWizardModel{
		profile: profile, backend: backend, defaultModel: defaultModel,
		registry: registry, step: setupWizardChoose,
		credential: newSetupCredentialInput(),
	}
}

func newSetupCredentialInput() textinput.Model {
	input := textinput.New()
	input.Prompt = "  > "
	input.CharLimit = 512
	input.EchoMode = textinput.EchoPassword
	input.EchoCharacter = '•'
	return input
}

func (m setupWizardModel) Init() tea.Cmd { return nil }

func (m setupWizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.done || m.err != nil {
		return m, tea.Quit
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.step == setupWizardCredential {
			return m.updateCredential(msg)
		}
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			m.err = fmt.Errorf("setup cancelled")
			return m, tea.Quit
		}
		if msg.String() == "esc" {
			switch m.step {
			case setupWizardCatalog, setupWizardConfirm, setupWizardHostedModels:
				m.step = setupWizardChoose
				return m, nil
			default:
				m.err = fmt.Errorf("setup cancelled")
				return m, tea.Quit
			}
		}
		switch msg.String() {
		case "up", "k":
			m.moveChoice(-1)
		case "down", "j":
			m.moveChoice(1)
		case "enter":
			return m.activateChoice()
		}
	case setupHostedModelsMsg:
		m.hosted = msg.models
		m.hostedInfo = msg.info
		if msg.err != nil {
			m.notice = fmt.Sprintf("Model discovery failed: %v. The default model is available.", msg.err)
			m.hosted = []string{m.defaultHost}
		}
		m.modelChoice = 0
		m.step = setupWizardHostedModels
		return m, nil
	}
	return m, nil
}

func (m setupWizardModel) updateCredential(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" || msg.String() == "esc" {
		m.step = setupWizardChoose
		m.credential.Blur()
		return m, nil
	}
	if msg.String() == "enter" {
		value := strings.TrimSpace(m.credential.Value())
		if value == "" {
			value = m.tokenEnv
		}
		m.tokenEnv, m.apiKey = hostedCredential(value, m.tokenEnv)
		if !validEnvironmentVariableName(m.tokenEnv) {
			m.notice = fmt.Sprintf("Invalid environment variable name %q.", m.tokenEnv)
			return m, nil
		}
		m.credential.Blur()
		return m, discoverSetupHostedModels(m.baseURL, m.tokenEnv, m.apiKey)
	}
	var cmd tea.Cmd
	m.credential, cmd = m.credential.Update(msg)
	return m, cmd
}

func discoverSetupHostedModels(baseURL, tokenEnv, apiKey string) tea.Cmd {
	return func() tea.Msg {
		if apiKey == "" && strings.TrimSpace(os.Getenv(tokenEnv)) == "" {
			return setupHostedModelsMsg{err: fmt.Errorf("%s is not set", tokenEnv)}
		}
		client := model.NewClient(baseURL, "")
		client.SetAuth("bearer", tokenEnv, "")
		client.SetAuthToken(apiKey)
		info, err := client.ListHostedModelInfo(context.Background(), strings.Contains(strings.ToLower(baseURL), "openrouter.ai"))
		models := make([]string, 0, len(info))
		for _, item := range info {
			models = append(models, item.ID)
		}
		return setupHostedModelsMsg{models: models, info: info, err: err}
	}
}

func (m *setupWizardModel) moveChoice(delta int) {
	limit := m.choiceLimit()
	if m.step == setupWizardCatalog {
		m.modelChoice = (m.modelChoice + delta + limit) % limit
		return
	}
	if m.step == setupWizardHostedModels {
		m.modelChoice = (m.modelChoice + delta + limit) % limit
		return
	}
	m.choice = (m.choice + delta + limit) % limit
}

func (m setupWizardModel) choiceLimit() int {
	switch m.step {
	case setupWizardCatalog:
		return maxSetupChoice(1, len(m.catalog))
	case setupWizardHostedModels:
		return maxSetupChoice(1, len(m.hosted))
	default:
		return 4
	}
}

func maxSetupChoice(minimum, value int) int {
	if value < minimum {
		return minimum
	}
	return value
}

func (m setupWizardModel) activateChoice() (tea.Model, tea.Cmd) {
	switch m.step {
	case setupWizardChoose:
		switch m.choice {
		case 0:
			m.target = setupTarget{backend: m.backend, model: m.defaultModel, local: true}
			m.notice = "Qodex will download this model only after you confirm."
			m.step = setupWizardConfirm
		case 1:
			catalog, err := m.registry.List()
			if err != nil {
				m.err = fmt.Errorf("list catalog models: %w", err)
				return m, tea.Quit
			}
			m.catalog = catalog
			m.modelChoice = 0
			m.step = setupWizardCatalog
		case 2, 3:
			m.provider = []string{"groq", "openrouter"}[m.choice-2]
			m.baseURL, m.defaultHost, m.tokenEnv = hostedWizardDefaults(m.provider)
			m.credential.SetValue("")
			m.credential.Focus()
			m.step = setupWizardCredential
		}
	case setupWizardCatalog:
		item := m.catalog[m.modelChoice]
		m.target = setupTarget{backend: m.backend, model: item.Name, local: true}
		m.notice = "Qodex will download this model only after you confirm."
		m.step = setupWizardConfirm
	case setupWizardConfirm:
		m.done = true
		if m.target.local && m.notice != "" {
			// Enter confirms the download choice.
		}
	case setupWizardHostedModels:
		m.target = setupTarget{backend: model.BackendExternal, model: m.hosted[m.modelChoice], baseURL: m.baseURL, authType: "bearer", tokenEnv: m.tokenEnv, apiKey: m.apiKey}
		m.done = true
	}
	if m.done {
		return m, tea.Quit
	}
	return m, nil
}

func hostedWizardDefaults(provider string) (baseURL, modelName, tokenEnv string) {
	if provider == "openrouter" {
		return "https://openrouter.ai/api/v1", "openai/gpt-oss-20b", "OPENROUTER_API_KEY"
	}
	return "https://api.groq.com/openai/v1", "llama-3.3-70b-versatile", "GROQ_API_KEY"
}

func (m setupWizardModel) View() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(setupTitleStyle.Render("Qodex Setup Wizard"))
	b.WriteString("\n")
	fmt.Fprintf(&b, "Profile: %s   Backend: %s\n\n", m.profile, m.backend)
	switch m.step {
	case setupWizardChoose:
		b.WriteString("How would you like to run Qodex?\n\n")
		choices := []string{
			fmt.Sprintf("Download recommended local model (%s)", m.defaultModel),
			"Choose another local catalog model",
			"Use Groq with BYOK",
			"Use OpenRouter with BYOK",
		}
		for i, choice := range choices {
			b.WriteString(setupWizardCursor(i == m.choice))
			b.WriteString(choice)
			b.WriteByte('\n')
		}
		b.WriteString("\n↑/↓ select  Enter continue  Esc cancel")
	case setupWizardCatalog:
		b.WriteString("Choose a local catalog model\n\n")
		for i, item := range m.catalog {
			b.WriteString(setupWizardCursor(i == m.modelChoice))
			fmt.Fprintf(&b, "%s (%s)\n", item.Name, item.Size)
		}
		b.WriteString("\n↑/↓ select  Enter continue  Esc back")
	case setupWizardConfirm:
		b.WriteString("Confirm local model download\n\n")
		fmt.Fprintf(&b, "Model: %s\n\n%s\n\n", m.target.model, m.notice)
		b.WriteString("Enter confirm  Esc back")
	case setupWizardCredential:
		fmt.Fprintf(&b, "Configure %s\n\n", m.provider)
		b.WriteString("Enter an environment-variable name or paste the API key.\n")
		b.WriteString(setupMutedStyle.Render("The input is hidden and the key will be stored in the OS credential store."))
		b.WriteString("\n\n")
		b.WriteString(m.credential.View())
		b.WriteString("\n\nEnter continue  Esc back")
		if m.notice != "" {
			b.WriteString("\n")
			b.WriteString(setupErrorStyle.Render(m.notice))
		}
	case setupWizardHostedModels:
		fmt.Fprintf(&b, "Choose a %s model\n\n", m.provider)
		if m.notice != "" {
			b.WriteString(setupMutedStyle.Render(m.notice))
			b.WriteString("\n\n")
		}
		for i, name := range m.hosted {
			b.WriteString(setupWizardCursor(i == m.modelChoice))
			label := name
			if i < len(m.hostedInfo) && m.hostedInfo[i].ID == name {
				label += "  [" + hostedModelBadge(m.hostedInfo[i]) + "]"
			}
			b.WriteString(label)
			b.WriteByte('\n')
		}
		b.WriteString("\n↑/↓ select  Enter continue  Esc back")
	}
	return b.String() + "\n"
}

func setupWizardCursor(selected bool) string {
	if selected {
		return setupChoiceStyle.Render("❯ ")
	}
	return "  "
}

func continueSetup(projectRoot string, profile setupProfile, backend model.Backend, defaultModel string, target setupTarget) error {
	registry := model.NewModelRegistry(getInstallRoot())
	if !target.local {
		return writeHostedSetup(projectRoot, target)
	}
	modelName := target.model
	installRoot := getInstallRoot()
	mgr := model.NewManager(backend, installRoot, modelName, 0)
	fmt.Printf("\nStep 3: Install Backend\nInstalling %s...\n", backend)
	if err := mgr.Install(context.Background()); err != nil {
		return fmt.Errorf("install %s: %w", backend, err)
	}
	fmt.Printf("  ✓ %s installed\n", backend)
	fmt.Println("\nStep 4: Preparing Model")
	ready, err := ensureSetupModel(context.Background(), registry, modelName, os.Stdout, os.Stderr)
	if err != nil || !ready {
		if err != nil {
			return err
		}
		return fmt.Errorf("model %q is not available", modelName)
	}
	mgr = model.NewManager(backend, installRoot, modelName, 0)
	mgr.SetContextTokens(config.Defaults(projectRoot).Runtime.ContextTokens)
	if err := mgr.EnsureRunning(context.Background()); err != nil {
		return fmt.Errorf("start %s: %w", backend, err)
	}
	cfg := config.Defaults(projectRoot)
	cfg.Runtime.Backend = string(backend)
	cfg.Model.BaseURL = fmt.Sprintf("http://127.0.0.1:%d/v1", mgr.Port())
	cfg.Model.Model = mgr.FindModelName()
	return writeSetupFiles(projectRoot, cfg)
}

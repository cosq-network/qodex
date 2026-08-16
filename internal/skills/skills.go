package skills

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/benoybose/qodex/internal/config"
	"github.com/pelletier/go-toml/v2"
)

const (
	maxKeywordSelectedSkills = 3
	maxModelSelectedSkills   = 2
	minimumSkillScore        = 10
)

// defaultAllowedTools is the small, project-oriented tool set exposed when
// no specialized skill has narrowed the registry. Keeping this bounded is
// important for native tool calling: sending every optional integration on
// every request wastes context and makes tool selection less reliable.
var defaultAllowedTools = []string{
	"list_files", "read_file", "search_text", "write_file", "write_patch",
	"run_command", "run_script", "run_tests", "run_formatter", "review_changes",
	"project_index", "lsp_diagnostics", "lsp_definition", "lsp_find_references",
	"git_status", "git_diff", "git_log", "git_workspace_summary", "git_stage",
	"git_commit", "git_branch", "git_worktree", "git_undo", "git_snapshot", "git_restore_snapshot",
}

type Metadata struct {
	Description   string   `toml:"description"`
	Triggers      []string `toml:"triggers"`
	AllowedTools  []string `toml:"allowed_tools"`
	ContextBudget int      `toml:"context_budget"`
	ContextTokens int      `toml:"context_budget_tokens"` // legacy alias kept for existing skills
	Scripts       []Script `toml:"scripts"`
}

// ToolDescriptor is the small amount of information the local matcher needs
// to decide whether an optional integration belongs in a turn.
type ToolDescriptor struct {
	Name        string
	Description string
}

type Match struct {
	Name    string
	Score   int
	Reasons []string
}

// Selection is the complete, deterministic per-turn routing decision.
type Selection struct {
	Skills      []Skill
	ActiveTools []string
	Matches     []Match
}

// Budget returns the skill's context budget in bytes, honoring the canonical
// `context_budget` key first and falling back to the legacy
// `context_budget_tokens` alias. A value of 0 means "no explicit budget".
func (m Metadata) Budget() int {
	if m.ContextBudget > 0 {
		return m.ContextBudget
	}
	return m.ContextTokens
}

type Script struct {
	Description string `toml:"description"`
	Command     string `toml:"command"`
	Tool        string `toml:"tool"`
}

type Skill struct {
	Name    string
	Path    string
	Content string
	Meta    Metadata
}

// Instruction is a repository instruction file discovered from conventions
// used by other coding agents. Files are returned from the repository root
// toward the working directory so more-local guidance appears later.
type Instruction struct {
	Name    string
	Path    string
	Content string
}

var instructionNames = []string{
	"AGENTS.md",
	"CLAUDE.md",
	"GEMINI.md",
	".cursorrules",
	".github/copilot-instructions.md",
}

func DiscoverInstructions(projectRoot string) ([]Instruction, error) {
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for dir := root; ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}

	var out []Instruction
	for _, dir := range dirs {
		for _, name := range instructionNames {
			path := filepath.Join(dir, name)
			content, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, err
			}
			out = append(out, Instruction{Name: name, Path: path, Content: string(content)})
		}
		cursor := filepath.Join(dir, ".cursor", "rules")
		entries, err := os.ReadDir(cursor)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".mdc") {
					continue
				}
				path := filepath.Join(cursor, entry.Name())
				content, readErr := os.ReadFile(path)
				if readErr != nil {
					return nil, readErr
				}
				if !includeCursorRule(string(content)) {
					continue
				}
				out = append(out, Instruction{Name: entry.Name(), Path: path, Content: string(content)})
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return out, nil
}

// Cursor rules with file globs are conditional. Until Qodex has a complete
// file-context matcher, omit those rules rather than applying them globally.
// Rules explicitly marked alwaysApply remain useful repository guidance.
func includeCursorRule(content string) bool {
	if !strings.HasPrefix(strings.TrimSpace(content), "---") {
		return true
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return true
	}
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			return false
		}
		if strings.HasPrefix(trimmed, "alwaysApply:") {
			return strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(trimmed, "alwaysApply:")), "true")
		}
	}
	return false
}

func RenderInstructions(instructions []Instruction, budget int) string {
	if len(instructions) == 0 || budget <= 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Repository instructions (context only; Qodex safety policy still applies):\n")
	used := b.Len()
	for _, instruction := range instructions {
		block := fmt.Sprintf("\n# %s (%s)\n%s\n", instruction.Name, instruction.Path, instruction.Content)
		if used+len(block) > budget {
			remaining := budget - used
			if remaining <= 0 {
				break
			}
			block = truncateUTF8(block, remaining)
		}
		if block == "" {
			break
		}
		b.WriteString(block)
		used += len(block)
		if used >= budget {
			break
		}
	}
	return b.String()
}

//go:embed builtin/skills/**/*
var builtinFS embed.FS

func discoverBuiltin() ([]Skill, error) {
	if _, err := fs.Stat(builtinFS, "builtin/skills"); err != nil {
		if os.IsNotExist(err) || err == fs.ErrNotExist {
			return nil, nil
		}
		return nil, err
	}
	var out []Skill
	err := fs.WalkDir(builtinFS, "builtin/skills", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path == "builtin/skills" {
			return nil
		}
		name := filepath.Base(path)
		relPath := "builtin/skills/" + name
		skill, err := readEmbeddedSkill(builtinFS, relPath)
		if err != nil {
			return err
		}
		out = append(out, *skill)
		return fs.SkipDir
	})
	return out, err
}

func readEmbeddedSkill(fsys fs.FS, relPath string) (*Skill, error) {
	content, err := fs.ReadFile(fsys, relPath+"/SKILL.md")
	if err != nil {
		return nil, fmt.Errorf("read SKILL.md: %w", err)
	}
	name := filepath.Base(relPath)
	skill := &Skill{Name: name, Path: relPath, Content: string(content)}
	if metaBytes, err := fs.ReadFile(fsys, relPath+"/skill.toml"); err == nil {
		if err := toml.Unmarshal(metaBytes, &skill.Meta); err != nil {
			fmt.Fprintf(os.Stderr, "[qodex] warning: skipping invalid skill.toml in %s: %v\n", relPath, err)
			skill.Meta = Metadata{}
		}
	}
	return skill, nil
}

func Discover(projectRoot string) ([]Skill, error) {
	builtins, err := discoverBuiltin()
	if err != nil {
		return nil, err
	}

	var roots []string
	roots = append(roots, filepath.Join(config.UserConfigDir(), "skills"))
	roots = append(roots, filepath.Join(projectRoot, ".qodex", "skills"))

	byName := map[string]Skill{}
	for _, skill := range builtins {
		byName[skill.Name] = skill
	}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			skillPath := filepath.Join(root, name)
			content, err := os.ReadFile(filepath.Join(skillPath, "SKILL.md"))
			if err != nil {
				continue
			}
			skill := Skill{Name: name, Path: skillPath, Content: string(content)}
			if metaBytes, err := os.ReadFile(filepath.Join(skillPath, "skill.toml")); err == nil {
				if err := toml.Unmarshal(metaBytes, &skill.Meta); err != nil {
					fmt.Fprintf(os.Stderr, "[qodex] warning: skipping invalid skill.toml in %s: %v\n", skillPath, err)
					skill.Meta = Metadata{}
				}
			}
			byName[name] = skill
		}
	}

	out := make([]Skill, 0, len(byName))
	for _, skill := range byName {
		out = append(out, skill)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func Select(all []Skill, prompt string) []Skill {
	return SelectWithTools(all, nil, prompt).Skills
}

// SelectWithTools performs all routing locally. Project instructions are
// always retained, while optional skills and external tools must match the
// current prompt. Results are stable for identical inputs.
func SelectWithTools(all []Skill, toolDescriptors []ToolDescriptor, prompt string) Selection {
	prompt = strings.ToLower(prompt)

	type scored struct {
		skill Skill
		score int
	}

	var project *Skill
	var scoredSkills []scored
	var matches []Match

	for _, skill := range all {
		name := strings.ToLower(skill.Name)
		if name == "project" {
			s := skill
			project = &s
			continue
		}
		score, reasons := scoreSkill(skill, prompt)
		if score >= minimumSkillScore {
			scoredSkills = append(scoredSkills, scored{skill, score})
			matches = append(matches, Match{Name: skill.Name, Score: score, Reasons: reasons})
		}
	}

	sort.Slice(scoredSkills, func(i, j int) bool {
		if scoredSkills[i].score != scoredSkills[j].score {
			return scoredSkills[i].score > scoredSkills[j].score
		}
		return scoredSkills[i].skill.Name < scoredSkills[j].skill.Name
	})
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Name < matches[j].Name
	})

	limit := maxKeywordSelectedSkills
	if project != nil {
		limit--
	}
	if len(scoredSkills) > limit {
		scoredSkills = scoredSkills[:limit]
	}

	result := make([]Skill, 0, maxKeywordSelectedSkills)
	if project != nil {
		result = append(result, *project)
	}
	for _, s := range scoredSkills {
		result = append(result, s.skill)
		if len(result) >= 3 {
			break
		}
	}

	active := ActiveTools(result, toolDescriptors, prompt)
	selectedNames := make(map[string]bool, len(result))
	for _, skill := range result {
		selectedNames[skill.Name] = true
	}
	selectedMatches := matches[:0]
	for _, match := range matches {
		if selectedNames[match.Name] {
			selectedMatches = append(selectedMatches, match)
		}
	}
	return Selection{Skills: result, ActiveTools: active, Matches: selectedMatches}
}

func matchScore(skill Skill, prompt string) int {
	score, _ := scoreSkill(skill, strings.ToLower(prompt))
	return score
}

func scoreSkill(skill Skill, prompt string) (int, []string) {
	score := 0
	var reasons []string
	promptLower := strings.ToLower(prompt)
	name := strings.ToLower(skill.Name)
	if explicitSkill(promptLower, name) {
		score += 10000
		reasons = append(reasons, "explicit /skill override")
	}

	for _, trigger := range skill.Meta.Triggers {
		if containsPhrase(promptLower, trigger) {
			score += 100
			reasons = append(reasons, "trigger: "+trigger)
		}
	}

	if containsPhrase(promptLower, name) {
		score += 80
		reasons = append(reasons, "skill name")
	}
	if containsPhrase(promptLower, skill.Meta.Description) {
		score += 30
		reasons = append(reasons, "description")
	}

	content := strings.ToLower(skill.Content)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")

	promptWords := meaningfulWords(promptLower)
	seen := map[string]bool{}
	bodyHits := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "//") {
			continue
		}
		words := strings.Fields(normalizeText(trimmed))
		isHeading := strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ")
		for _, w := range words {
			if !meaningfulWord(w) || seen[w] {
				continue
			}
			seen[w] = true
			if promptWords[w] {
				if isHeading {
					score += 10
					reasons = append(reasons, "heading: "+w)
				} else {
					bodyHits++
				}
			}
		}
	}
	if bodyHits > 0 {
		score += bodyHits * 4
		reasons = append(reasons, fmt.Sprintf("content keywords: %d", bodyHits))
	}

	return score, reasons
}

func containsPhrase(text, phrase string) bool {
	textWords := strings.Fields(normalizeText(text))
	phraseWords := strings.Fields(normalizeText(phrase))
	if len(phraseWords) == 0 || len(phraseWords) > len(textWords) {
		return false
	}
	for i := 0; i <= len(textWords)-len(phraseWords); i++ {
		matched := true
		for j := range phraseWords {
			if !matchWord(textWords[i+j], phraseWords[j]) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func matchWord(text, phrase string) bool {
	return text == phrase || text == phrase+"s"
}

func explicitSkill(prompt, name string) bool {
	return containsPhrase(prompt, "/skill "+name)
}

var ignoredMatchWords = map[string]bool{
	"about": true, "after": true, "before": true, "from": true, "have": true,
	"into": true, "just": true, "like": true, "need": true, "please": true,
	"that": true, "than": true, "this": true, "when": true, "with": true,
}

func normalizeText(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

func normalizeWord(value string) string {
	return strings.TrimSpace(normalizeText(value))
}

func meaningfulWord(word string) bool {
	return len(word) >= 4 && !ignoredMatchWords[word]
}

func meaningfulWords(text string) map[string]bool {
	words := make(map[string]bool)
	for _, word := range strings.Fields(normalizeText(text)) {
		if meaningfulWord(word) {
			words[word] = true
		}
	}
	return words
}

// ActiveTools returns core tools plus the union of selected skill packs and
// matching MCP descriptors. Unknown names are retained so the agent can
// report a useful dispatch error, while the registry remains authoritative.
func ActiveTools(selected []Skill, descriptors []ToolDescriptor, prompt string) []string {
	set := make(map[string]bool, len(defaultAllowedTools))
	for _, name := range defaultAllowedTools {
		set[name] = true
	}
	mcpMatches := make(map[string]bool, len(descriptors))
	for _, tool := range descriptors {
		mcpMatches[tool.Name] = descriptorMatches(strings.ToLower(prompt), tool)
		if mcpMatches[tool.Name] {
			set[tool.Name] = true
		}
	}
	for _, skill := range selected {
		for _, name := range skill.Meta.AllowedTools {
			if strings.HasPrefix(name, "mcp_") && !mcpMatches[name] {
				continue
			}
			set[name] = true
		}
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func descriptorMatches(prompt string, tool ToolDescriptor) bool {
	if containsPhrase(prompt, tool.Name) || containsPhrase(prompt, tool.Description) {
		return true
	}
	// MCP names commonly use underscores while users use spaces. Match
	// meaningful name/description words without letting stop words select a
	// remote tool accidentally.
	terms := func(value string) []string {
		value = strings.NewReplacer("_", " ", "-", " ", "/", " ").Replace(strings.ToLower(value))
		var out []string
		for _, word := range strings.Fields(value) {
			word = strings.Trim(word, ".,:;!?()[]{}'\"`")
			if len(word) >= 4 && word != "with" && word != "from" && word != "that" {
				out = append(out, word)
			}
		}
		return out
	}
	for _, candidate := range append(terms(tool.Name), terms(tool.Description)...) {
		if meaningfulWords(prompt)[candidate] {
			return true
		}
	}
	return false
}

func Summarize(skills []Skill) string {
	var b strings.Builder
	for _, s := range skills {
		b.WriteString("- ")
		b.WriteString(s.Name)
		if len(s.Meta.Triggers) > 0 {
			b.WriteString(" (triggers: ")
			b.WriteString(strings.Join(s.Meta.Triggers, ", "))
			b.WriteString(")")
		}
		firstLine := strings.SplitN(strings.ReplaceAll(strings.TrimSpace(s.Content), "\r\n", "\n"), "\n", 2)[0]
		firstLine = strings.TrimLeft(firstLine, "# ")
		if firstLine != "" {
			b.WriteString(": ")
			b.WriteString(firstLine)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func SelectViaModel(ctx context.Context, all []Skill, prompt string, ask func(ctx context.Context, msg string) (string, error)) ([]Skill, error) {

	summaries := Summarize(all)
	selectionPrompt := `You are a skill router. Select the most relevant skills for the given task.

Available skills:
` + summaries + `
Task: ` + prompt + `

Respond with ONLY a JSON object containing the names of relevant skills:
{"skills": ["name1", "name2"]}
Return an empty array if no skills are relevant.`

	resp, err := ask(ctx, selectionPrompt)
	if err != nil {
		return nil, err
	}

	cleaned := strings.TrimSpace(resp)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var result struct {
		Skills []string `json:"skills"`
	}
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("parse selection: %w", err)
	}

	byName := make(map[string]Skill, len(all))
	for _, s := range all {
		byName[s.Name] = s
	}

	var project *Skill
	var out []Skill
	anyRestrict := false
	for _, name := range result.Skills {
		if s, ok := byName[name]; ok {
			if strings.ToLower(name) == "project" {
				continue
			}
			if len(s.Meta.AllowedTools) > 0 {
				anyRestrict = true
			}
			if len(out) < maxModelSelectedSkills {
				out = append(out, s)
			}
		}
	}
	for _, s := range all {
		if strings.ToLower(s.Name) == "project" {
			p := s
			project = &p
			break
		}
	}

	if anyRestrict && len(out) == 0 {
		return nil, nil
	}

	resultSkills := make([]Skill, 0, 3)
	if project != nil {
		resultSkills = append(resultSkills, *project)
	}
	resultSkills = append(resultSkills, out...)
	return resultSkills, nil
}

func truncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for !utf8.ValidString(s[:cut]) && cut > 0 {
		cut--
	}
	return s[:cut]
}

func Render(skills []Skill, budget int) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Loaded skills:\n")
	used := 0
	for _, skill := range skills {
		content := skill.Content
		skillBudget := budget / len(skills)
		if per := skill.Meta.Budget(); per > 0 && per < skillBudget {
			skillBudget = per
		}
		if len(content) > skillBudget {
			content = truncateUTF8(content, skillBudget)
		}
		used += len(content)
		if used > budget {
			break
		}
		b.WriteString("\n# Skill: ")
		b.WriteString(skill.Name)
		b.WriteString("\n")
		b.WriteString(content)
		b.WriteString("\n")
	}
	return b.String()
}

func AllowedTools(selected []Skill) []string {
	var intersection []string
	anyRestrict := false
	for i, skill := range selected {
		if len(skill.Meta.AllowedTools) > 0 {
			if !anyRestrict {
				intersection = make([]string, len(skill.Meta.AllowedTools))
				copy(intersection, skill.Meta.AllowedTools)
				anyRestrict = true
			} else {
				intersection = intersectStrings(intersection, skill.Meta.AllowedTools)
			}
			_ = i
		}
	}
	if !anyRestrict {
		return append([]string(nil), defaultAllowedTools...)
	}
	return intersection
}

func Scripts(selected []Skill) []Script {
	seen := map[string]bool{}
	var out []Script
	for _, skill := range selected {
		for _, script := range skill.Meta.Scripts {
			if seen[script.Description] {
				continue
			}
			seen[script.Description] = true
			out = append(out, script)
		}
	}
	return out
}

type section struct {
	heading string
	body    string
	score   int
}

func splitSections(content string) (preamble string, sections []section) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	var current *section
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") && !strings.HasPrefix(line, "### ") {
			if current != nil {
				sections = append(sections, *current)
			}
			current = &section{heading: line}
			continue
		}
		if current != nil {
			if current.body != "" {
				current.body += "\n"
			}
			current.body += line
		} else {
			if preamble != "" {
				preamble += "\n"
			}
			preamble += line
		}
	}
	if current != nil {
		sections = append(sections, *current)
	}
	return
}

func scoreSectionPrompt(s section, promptLower string) int {
	score := 0
	seen := map[string]bool{}

	headingWords := strings.Fields(strings.ToLower(s.heading))
	for _, w := range headingWords {
		w = strings.Trim(w, "# .,:;!?()[]")
		if len(w) < 2 || seen[w] {
			continue
		}
		seen[w] = true
		if strings.Contains(promptLower, w) {
			score += 5
		}
	}

	bodyWords := strings.Fields(strings.ToLower(s.body))
	for _, w := range bodyWords {
		w = strings.Trim(w, ".,:;!?()[]{}'\"`")
		if len(w) < 4 || seen[w] {
			continue
		}
		seen[w] = true
		if strings.Contains(promptLower, w) {
			score += 1
		}
	}

	return score
}

func RenderSliced(skills []Skill, prompt string, budget int) string {
	if len(skills) == 0 {
		return ""
	}
	promptLower := strings.ToLower(prompt)
	var b strings.Builder
	b.WriteString("Loaded skills:\n")
	used := 0

	for _, skill := range skills {
		skillBudget := budget / len(skills)
		if per := skill.Meta.Budget(); per > 0 && per < skillBudget {
			skillBudget = per
		}
		if skillBudget <= 0 {
			continue
		}

		preamble, sections := splitSections(skill.Content)
		remaining := skillBudget

		b.WriteString("\n# Skill: ")
		b.WriteString(skill.Name)
		b.WriteString("\n")
		if preamble != "" {
			preambleLen := len(preamble)
			if preambleLen > remaining {
				preambleLen = remaining
			}
			if preambleLen > 0 {
				b.WriteString(truncateUTF8(preamble, preambleLen))
				b.WriteString("\n")
				remaining -= preambleLen
				used += preambleLen
			}
		}

		if remaining > 0 && len(sections) > 0 {
			for i := range sections {
				sections[i].score = scoreSectionPrompt(sections[i], promptLower)
			}
			sort.Slice(sections, func(i, j int) bool {
				return sections[i].score > sections[j].score
			})

			for _, sec := range sections {
				if remaining <= 0 {
					break
				}
				secContent := sec.heading
				if sec.body != "" {
					secContent += "\n" + sec.body
				}
				secLen := len(secContent)
				if secLen > remaining {
					secLen = remaining
				}
				if secLen > 0 {
					b.WriteString(truncateUTF8(secContent, secLen))
					b.WriteString("\n")
					remaining -= secLen
					used += secLen
				}
			}
		}

		if used >= budget {
			break
		}
	}

	return b.String()
}

func intersectStrings(a, b []string) []string {
	set := make(map[string]bool, len(b))
	for _, s := range b {
		set[s] = true
	}
	var out []string
	for _, s := range a {
		if set[s] {
			out = append(out, s)
		}
	}
	return out
}

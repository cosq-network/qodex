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

	"github.com/pelletier/go-toml/v2"
)

const (
	maxKeywordSelectedSkills = 3
	maxModelSelectedSkills   = 2
)

type Metadata struct {
	Triggers      []string `toml:"triggers"`
	AllowedTools  []string `toml:"allowed_tools"`
	ContextBudget int      `toml:"context_budget"`
	Scripts       []Script `toml:"scripts"`
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
	home, _ := os.UserHomeDir()
	if home != "" {
		roots = append(roots, filepath.Join(home, ".config", "qodex", "skills"))
	}
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
	prompt = strings.ToLower(prompt)

	type scored struct {
		skill Skill
		score int
	}

	var project *Skill
	var scoredSkills []scored

	for _, skill := range all {
		name := strings.ToLower(skill.Name)
		if name == "project" {
			s := skill
			project = &s
			continue
		}
		score := matchScore(skill, prompt)
		if score > 0 || strings.Contains(prompt, "/skill "+name) {
			if score < 1 {
				score = 1
			}
			scoredSkills = append(scoredSkills, scored{skill, score})
		}
	}

	sort.Slice(scoredSkills, func(i, j int) bool {
		return scoredSkills[i].score > scoredSkills[j].score
	})

	if len(scoredSkills) > maxKeywordSelectedSkills-1 {
		scoredSkills = scoredSkills[:maxKeywordSelectedSkills-1]
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

	return result
}

func matchScore(skill Skill, prompt string) int {
	score := 0
	promptLower := strings.ToLower(prompt)

	for _, trigger := range skill.Meta.Triggers {
		if strings.Contains(promptLower, strings.ToLower(trigger)) {
			score += 20
		}
	}

	name := strings.ToLower(skill.Name)
	if strings.Contains(promptLower, name) {
		score += 10
	}

	content := strings.ToLower(skill.Content)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")

	seen := map[string]bool{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "//") {
			continue
		}
		words := strings.Fields(trimmed)
		isHeading := strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ")
		for _, w := range words {
			if isHeading {
				w = strings.Trim(w, ".,:;!?()[]")
			} else {
				w = strings.Trim(w, ".,:;!?()[]{}'\"`")
			}
			w = strings.ToLower(w)
			if len(w) < 4 || seen[w] {
				continue
			}
			seen[w] = true
			if strings.Contains(promptLower, w) {
				if isHeading {
					score += 3
				} else {
					score += 1
				}
			}
		}
	}

	return score
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
		if skill.Meta.ContextBudget > 0 && skill.Meta.ContextBudget < skillBudget {
			skillBudget = skill.Meta.ContextBudget
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
		return nil
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
		if skill.Meta.ContextBudget > 0 && skill.Meta.ContextBudget < skillBudget {
			skillBudget = skill.Meta.ContextBudget
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

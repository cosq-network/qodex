package skills

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Skill struct {
	Name    string
	Path    string
	Content string
}

func Discover(projectRoot string) ([]Skill, error) {
	var roots []string
	home, _ := os.UserHomeDir()
	if home != "" {
		roots = append(roots, filepath.Join(home, ".config", "locha", "skills"))
	}
	roots = append(roots, filepath.Join(projectRoot, ".locha", "skills"))

	byName := map[string]Skill{}
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
			byName[name] = Skill{Name: name, Path: skillPath, Content: string(content)}
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
	var selected []Skill
	for _, skill := range all {
		if strings.Contains(prompt, strings.ToLower(skill.Name)) || strings.Contains(prompt, "/skill "+strings.ToLower(skill.Name)) {
			selected = append(selected, skill)
		}
	}
	if len(selected) > 3 {
		return selected[:3]
	}
	return selected
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
		if len(content) > budget/len(skills) {
			content = content[:budget/len(skills)]
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

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

	if len(scoredSkills) > 2 {
		scoredSkills = scoredSkills[:2]
	}

	result := make([]Skill, 0, 3)
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

	name := strings.ToLower(skill.Name)
	if strings.Contains(prompt, name) {
		score += 10
	}

	content := strings.ToLower(skill.Content)
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
			if strings.Contains(prompt, w) {
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

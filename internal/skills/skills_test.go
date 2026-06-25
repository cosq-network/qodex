package skills

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestSelectProjectAlwaysIncluded(t *testing.T) {
	skills := []Skill{
		{Name: "project", Content: "# Project\n\nFollow repo conventions."},
		{Name: "go", Content: "# Go\n\nUse go module conventions."},
	}
	result := Select(skills, "write a test")
	if len(result) == 0 {
		t.Fatal("expected at least project skill")
	}
	if result[0].Name != "project" {
		t.Fatalf("expected project first, got %s", result[0].Name)
	}
}

func TestSelectNameMatch(t *testing.T) {
	skills := []Skill{
		{Name: "project", Content: "# Project"},
		{Name: "python", Content: "# Python\n\nUse snake_case."},
	}
	result := Select(skills, "write a python script")
	if len(result) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(result))
	}
	if result[1].Name != "python" {
		t.Fatalf("expected python matched, got %s", result[1].Name)
	}
}

func TestSelectContentTriggerMatch(t *testing.T) {
	skills := []Skill{
		{Name: "project", Content: "# Project"},
		{Name: "testing", Content: "# Testing\n\nWrite table-driven tests."},
	}
	result := Select(skills, "how do I write table-driven tests")
	if len(result) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(result))
	}
	if result[1].Name != "testing" {
		t.Fatalf("expected testing matched via content, got %s", result[1].Name)
	}
}

func TestSelectExplicitSlashSkill(t *testing.T) {
	skills := []Skill{
		{Name: "project", Content: "# Project"},
		{Name: "react", Content: "# React\n\nUse hooks."},
	}
	result := Select(skills, "/skill react build a component")
	if len(result) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(result))
	}
	if result[1].Name != "react" {
		t.Fatalf("expected react, got %s", result[1].Name)
	}
}

func TestSelectCapAtThree(t *testing.T) {
	skills := []Skill{
		{Name: "project", Content: "# Project"},
		{Name: "go", Content: "# Go"},
		{Name: "rust", Content: "# Rust"},
		{Name: "python", Content: "# Python"},
	}
	result := Select(skills, "go rust python")
	if len(result) > 3 {
		t.Fatalf("expected at most 3 skills, got %d", len(result))
	}
	if result[0].Name != "project" {
		t.Fatalf("expected project first, got %s", result[0].Name)
	}
}

func TestSelectNoMatchReturnsProjectOnly(t *testing.T) {
	skills := []Skill{
		{Name: "project", Content: "# Project"},
		{Name: "database", Content: "# Database\n\nUse SQL."},
	}
	result := Select(skills, "deploy the application")
	if len(result) != 1 {
		t.Fatalf("expected only project skill, got %d", len(result))
	}
	if result[0].Name != "project" {
		t.Fatalf("expected project, got %s", result[0].Name)
	}
}

func TestSelectNoProjectSkill(t *testing.T) {
	skills := []Skill{
		{Name: "go", Content: "# Go\n\nUse go modules."},
	}
	result := Select(skills, "write go code")
	if len(result) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(result))
	}
	if result[0].Name != "go" {
		t.Fatalf("expected go, got %s", result[0].Name)
	}
}

func TestMatchScoreNameMatch(t *testing.T) {
	skill := Skill{Name: "python", Content: "# Python\n\nWrite scripts."}
	score := matchScore(skill, "write a python script")
	if score < 10 {
		t.Fatalf("expected name match score >= 10, got %d", score)
	}
}

func TestMatchScoreContentTrigger(t *testing.T) {
	skill := Skill{Name: "testing", Content: "# Testing\n\nUse table-driven tests."}
	score := matchScore(skill, "show me examples of table-driven tests")
	if score < 1 {
		t.Fatalf("expected content trigger score >= 1, got %d", score)
	}
}

func TestMatchScoreHeadingAndBody(t *testing.T) {
	skill := Skill{Name: "database", Content: "# Database\n\nUse connection pooling."}
	score := matchScore(skill, "configure database connection pooling")
	if score < 4 {
		t.Fatalf("expected score from name+heading+body >= 4, got %d", score)
	}
}

func TestRenderBudget(t *testing.T) {
	skills := []Skill{
		{Name: "project", Content: "# Project\n\n" + string(make([]byte, 5000))},
	}
	result := Render(skills, 100)
	if len(result) == 0 || len(result) > 150 {
		t.Fatalf("Render budget not respected: len=%d", len(result))
	}
}

func TestRenderPerSkillBudget(t *testing.T) {
	skills := []Skill{
		{Name: "project", Content: "# Project\n\n" + string(make([]byte, 200))},
		{Name: "go", Content: "# Go\n\n" + string(make([]byte, 200)), Meta: Metadata{ContextBudget: 50}},
	}
	result := Render(skills, 8000)
	if len(result) > 500 {
		t.Fatalf("expected go skill to be truncated to 50 bytes, total len=%d", len(result))
	}
}

func TestAllowedToolsNoRestriction(t *testing.T) {
	skills := []Skill{
		{Name: "project", Content: "# Project"},
	}
	result := AllowedTools(skills)
	if result != nil {
		t.Fatalf("expected nil when no skill restricts tools, got %v", result)
	}
}

func TestAllowedToolsIntersection(t *testing.T) {
	skills := []Skill{
		{Name: "project", Content: "# Project", Meta: Metadata{AllowedTools: []string{"read_file", "write_file", "list_files"}}},
		{Name: "safe", Content: "# Safe", Meta: Metadata{AllowedTools: []string{"read_file", "search_text", "list_files"}}},
	}
	result := AllowedTools(skills)
	if len(result) != 2 {
		t.Fatalf("expected 2 intersected tools, got %v", result)
	}
	for _, name := range result {
		if name != "read_file" && name != "list_files" {
			t.Fatalf("unexpected tool in intersection: %s", name)
		}
	}
}

func TestAllowedToolsSingleSkill(t *testing.T) {
	skills := []Skill{
		{Name: "project", Content: "# Project"},
		{Name: "restricted", Content: "# Restricted", Meta: Metadata{AllowedTools: []string{"read_file"}}},
	}
	result := AllowedTools(skills)
	if len(result) != 1 || result[0] != "read_file" {
		t.Fatalf("expected [read_file], got %v", result)
	}
}

func TestScriptsCollect(t *testing.T) {
	skills := []Skill{
		{Name: "project", Content: "# Project", Meta: Metadata{
			Scripts: []Script{
				{Description: "Run tests", Command: "go test ./...", Tool: "run_command"},
			},
		}},
		{Name: "fmt", Content: "# Fmt", Meta: Metadata{
			Scripts: []Script{
				{Description: "Format code", Command: "gofmt -w .", Tool: "run_command"},
			},
		}},
	}
	result := Scripts(skills)
	if len(result) != 2 {
		t.Fatalf("expected 2 scripts, got %d", len(result))
	}
}

func TestMatchScoreTriggers(t *testing.T) {
	skill := Skill{
		Name:    "go",
		Content: "# Go",
		Meta:    Metadata{Triggers: []string{"goroutine", "golang", "defer"}},
	}
	score := matchScore(skill, "explain goroutines to me")
	if score < 20 {
		t.Fatalf("expected trigger score >= 20, got %d", score)
	}
}

func TestSummarize(t *testing.T) {
	skills := []Skill{
		{Name: "project", Content: "# Project\nRepo conventions."},
		{Name: "go", Content: "# Go\nGo module conventions.", Meta: Metadata{Triggers: []string{"go", "golang", "goroutine"}}},
	}
	s := Summarize(skills)
	if !strings.Contains(s, "project") || !strings.Contains(s, "go") {
		t.Fatalf("summary missing skills: %s", s)
	}
	if !strings.Contains(s, "goroutine") {
		t.Fatalf("summary missing triggers: %s", s)
	}
}

func TestSelectViaModelSelectsRelevant(t *testing.T) {
	all := []Skill{
		{Name: "project", Content: "# Project\nRepo conventions."},
		{Name: "python", Content: "# Python\nPython development.", Meta: Metadata{Triggers: []string{"python", "pip", "django"}}},
		{Name: "go", Content: "# Go\nGo module conventions.", Meta: Metadata{Triggers: []string{"go", "golang", "goroutine"}}},
	}

	ask := func(ctx context.Context, msg string) (string, error) {
		return `{"skills": ["go"]}`, nil
	}

	result, err := SelectViaModel(all, "write a goroutine", ask)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 skills (project + go), got %d", len(result))
	}
	if result[0].Name != "project" || result[1].Name != "go" {
		t.Fatalf("expected [project go], got %v", result)
	}
}

func TestSelectViaModelEmptyReturnsProjectOnly(t *testing.T) {
	all := []Skill{
		{Name: "project", Content: "# Project\nRepo conventions."},
		{Name: "rust", Content: "# Rust\nRust development."},
	}

	ask := func(ctx context.Context, msg string) (string, error) {
		return `{"skills": []}`, nil
	}

	result, err := SelectViaModel(all, "deploy the app", ask)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Name != "project" {
		t.Fatalf("expected only project, got %v", result)
	}
}

func TestSelectViaModelFallbackOnParseError(t *testing.T) {
	all := []Skill{
		{Name: "project", Content: "# Project"},
	}

	ask := func(ctx context.Context, msg string) (string, error) {
		return "not json", nil
	}

	_, err := SelectViaModel(all, "test", ask)
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

func TestSelectViaModelIgnoresProjectInResponse(t *testing.T) {
	all := []Skill{
		{Name: "project", Content: "# Project"},
		{Name: "foo", Content: "# Foo"},
	}

	ask := func(ctx context.Context, msg string) (string, error) {
		return `{"skills": ["project", "foo"]}`, nil
	}

	result, err := SelectViaModel(all, "foo task", ask)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[0].Name != "project" || result[1].Name != "foo" {
		t.Fatalf("expected [project foo], got %v", result)
	}
}

func TestSelectViaModelReturnsNilWhenEmptyAndRestricted(t *testing.T) {
	all := []Skill{
		{Name: "project", Content: "# Project"},
		{Name: "restricted", Content: "# Restricted", Meta: Metadata{AllowedTools: []string{"read_file"}}},
	}

	ask := func(ctx context.Context, msg string) (string, error) {
		return `{"skills": []}`, nil
	}

	result, err := SelectViaModel(all, "task", ask)
	if err != nil {
		t.Fatal(err)
	}
	// project has no AllowedTools, restricted wasn't selected, so no skills restrict
	// but result should still be [project]
	if len(result) != 1 || result[0].Name != "project" {
		t.Fatalf("expected [project], got %v", result)
	}
}

func TestSelectViaModelCapsAtThree(t *testing.T) {
	all := []Skill{
		{Name: "project", Content: "# Project"},
		{Name: "a", Content: "# A"},
		{Name: "b", Content: "# B"},
		{Name: "c", Content: "# C"},
	}

	ask := func(ctx context.Context, msg string) (string, error) {
		return `{"skills": ["a", "b", "c"]}`, nil
	}

	result, err := SelectViaModel(all, "task", ask)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) > 3 {
		t.Fatalf("expected at most 3, got %d", len(result))
	}
}

func TestSplitSections(t *testing.T) {
	content := "# Overview\nSome intro.\n## Section One\nBody one.\n### Subsection\nSub body.\n## Section Two\nBody two."
	preamble, sections := splitSections(content)
	if !strings.Contains(preamble, "Overview") {
		t.Fatalf("preamble missing: %q", preamble)
	}
	if len(sections) != 3 {
		t.Fatalf("expected 3 sections, got %d", len(sections))
	}
	if sections[0].heading != "## Section One" {
		t.Fatalf("expected section one heading, got %q", sections[0].heading)
	}
	if sections[2].heading != "## Section Two" {
		t.Fatalf("expected section two heading, got %q", sections[2].heading)
	}
}

func TestSplitSectionsNoHeadings(t *testing.T) {
	preamble, sections := splitSections("Just a flat skill content.")
	if preamble != "Just a flat skill content." {
		t.Fatalf("expected preamble, got %q", preamble)
	}
	if len(sections) != 0 {
		t.Fatalf("expected no sections, got %d", len(sections))
	}
}

func TestScoreSectionPrompt(t *testing.T) {
	sec := section{
		heading: "## Connection Pooling",
		body:    "Use connection pooling for database access.",
	}
	score := scoreSectionPrompt(sec, "configure database connection pooling")
	if score < 5 {
		t.Fatalf("expected heading match score >= 5, got %d", score)
	}
}

func TestRenderSlicedIncludesPreamble(t *testing.T) {
	skills := []Skill{
		{
			Name: "project",
			Content: "# Project\n\nRepo conventions.\n" +
				"## Go\nUse go modules.\n" +
				"## Python\nUse snake_case.\n" +
				"## Database\nUse SQL.",
		},
	}
	result := RenderSliced(skills, "write python code", 8000)
	if !strings.Contains(result, "Repo conventions") {
		t.Fatalf("preamble missing: %s", result)
	}
	if !strings.Contains(result, "Python") {
		t.Fatalf("expected Python section, got: %s", result)
	}
}

func TestRenderSlicedSelectsRelevantSection(t *testing.T) {
	skills := []Skill{
		{
			Name: "project",
			Content: "# Project\n\nRules.\n" +
				"## Conventions\nGo conventions.\n" +
				"## Deployment\nDeploy steps.\n" +
				"## Database\nSQL setup.",
		},
	}
	// Prompt matches "Conventions" heading via "conventions" keyword
	result := RenderSliced(skills, "conventions setup", 8000)
	if !strings.Contains(result, "Rules") {
		t.Fatalf("preamble missing: %s", result)
	}
	if !strings.Contains(result, "Conventions") {
		t.Fatalf("expected Conventions section to be included, got: %s", result)
	}
}

func TestRenderSlicedObeysBudgetIncludingPreamble(t *testing.T) {
	skills := []Skill{
		{
			Name: "project",
			Content: "# Project\n\n" + strings.Repeat("A", 100) + "\n" +
				"## Section\nbody",
		},
	}
	// Budget only covers preamble
	result := RenderSliced(skills, "section", 50)
	if strings.Contains(result, "Section") {
		t.Fatalf("expected section excluded due to budget, got: %s", result)
	}
}

func TestRenderSlicedRespectsPerSkillBudget(t *testing.T) {
	skills := []Skill{
		{
			Name:    "project",
			Content: "# Project\n\n" + strings.Repeat("x", 500) + "\n## Section\nbody",
			Meta:    Metadata{ContextBudget: 50},
		},
	}
	result := RenderSliced(skills, "section", 8000)
	if len(result) > 200 {
		t.Fatalf("expected tight budget, got len=%d", len(result))
	}
}

func TestRenderSlicedEmpty(t *testing.T) {
	if r := RenderSliced(nil, "test", 100); r != "" {
		t.Fatalf("expected empty, got %q", r)
	}
}

func TestDiscoverWithSkillToml(t *testing.T) {
	dir := t.TempDir()
	skillDir := dir + "/.qodex/skills/myskill"
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillDir+"/SKILL.md", []byte("# My Skill"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillDir+"/skill.toml", []byte(`triggers = ["foo", "bar"]
allowed_tools = ["read_file"]
context_budget = 500
[[scripts]]
description = "Say hello"
command = "echo hello"
tool = "run_command"
`), 0644); err != nil {
		t.Fatal(err)
	}

	skills, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "myskill" {
		t.Fatalf("expected myskill, got %s", skills[0].Name)
	}
	if len(skills[0].Meta.Triggers) != 2 || skills[0].Meta.Triggers[0] != "foo" {
		t.Fatalf("unexpected triggers: %v", skills[0].Meta.Triggers)
	}
	if skills[0].Meta.ContextBudget != 500 {
		t.Fatalf("expected budget 500, got %d", skills[0].Meta.ContextBudget)
	}
	if len(skills[0].Meta.Scripts) != 1 || skills[0].Meta.Scripts[0].Description != "Say hello" {
		t.Fatalf("unexpected scripts: %v", skills[0].Meta.Scripts)
	}
}

func TestRenderEmpty(t *testing.T) {
	if r := Render(nil, 100); r != "" {
		t.Fatalf("expected empty, got %q", r)
	}
}

package skills

import (
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

func TestRenderEmpty(t *testing.T) {
	if r := Render(nil, 100); r != "" {
		t.Fatalf("expected empty, got %q", r)
	}
}

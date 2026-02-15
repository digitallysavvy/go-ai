package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestSkillRegistry_Register(t *testing.T) {
	registry := NewSkillRegistry()

	skill := &Skill{
		Name:        "test-skill",
		Description: "A test skill",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "result", nil
		},
	}

	// Test successful registration
	err := registry.Register(skill)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Test duplicate registration
	err = registry.Register(skill)
	if err == nil {
		t.Fatal("expected error for duplicate registration, got nil")
	}

	// Test nil skill
	err = registry.Register(nil)
	if err == nil {
		t.Fatal("expected error for nil skill, got nil")
	}

	// Test empty name
	emptyNameSkill := &Skill{
		Name: "",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "", nil
		},
	}
	err = registry.Register(emptyNameSkill)
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}

	// Test nil handler
	nilHandlerSkill := &Skill{
		Name:    "nil-handler",
		Handler: nil,
	}
	err = registry.Register(nilHandlerSkill)
	if err == nil {
		t.Fatal("expected error for nil handler, got nil")
	}
}

func TestSkillRegistry_Get(t *testing.T) {
	registry := NewSkillRegistry()

	skill := &Skill{
		Name:        "test-skill",
		Description: "A test skill",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "result", nil
		},
	}

	registry.Register(skill)

	// Test getting existing skill
	retrieved, exists := registry.Get("test-skill")
	if !exists {
		t.Fatal("expected skill to exist")
	}
	if retrieved.Name != "test-skill" {
		t.Fatalf("expected name 'test-skill', got: %s", retrieved.Name)
	}

	// Test getting non-existent skill
	_, exists = registry.Get("non-existent")
	if exists {
		t.Fatal("expected skill not to exist")
	}
}

func TestSkillRegistry_Has(t *testing.T) {
	registry := NewSkillRegistry()

	skill := &Skill{
		Name:        "test-skill",
		Description: "A test skill",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "result", nil
		},
	}

	registry.Register(skill)

	if !registry.Has("test-skill") {
		t.Fatal("expected skill to exist")
	}

	if registry.Has("non-existent") {
		t.Fatal("expected skill not to exist")
	}
}

func TestSkillRegistry_Unregister(t *testing.T) {
	registry := NewSkillRegistry()

	skill := &Skill{
		Name:        "test-skill",
		Description: "A test skill",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "result", nil
		},
	}

	registry.Register(skill)

	if !registry.Has("test-skill") {
		t.Fatal("expected skill to exist")
	}

	registry.Unregister("test-skill")

	if registry.Has("test-skill") {
		t.Fatal("expected skill to be removed")
	}
}

func TestSkillRegistry_List(t *testing.T) {
	registry := NewSkillRegistry()

	skill1 := &Skill{
		Name:        "skill-1",
		Description: "First skill",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "result1", nil
		},
	}

	skill2 := &Skill{
		Name:        "skill-2",
		Description: "Second skill",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "result2", nil
		},
	}

	registry.Register(skill1)
	registry.Register(skill2)

	skills := registry.List()
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got: %d", len(skills))
	}
}

func TestSkillRegistry_Names(t *testing.T) {
	registry := NewSkillRegistry()

	skill1 := &Skill{
		Name:        "skill-1",
		Description: "First skill",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "result1", nil
		},
	}

	skill2 := &Skill{
		Name:        "skill-2",
		Description: "Second skill",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "result2", nil
		},
	}

	registry.Register(skill1)
	registry.Register(skill2)

	names := registry.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got: %d", len(names))
	}

	// Check that both names are present
	nameSet := make(map[string]bool)
	for _, name := range names {
		nameSet[name] = true
	}

	if !nameSet["skill-1"] || !nameSet["skill-2"] {
		t.Fatal("expected both skill-1 and skill-2 in names")
	}
}

func TestSkillRegistry_Execute(t *testing.T) {
	registry := NewSkillRegistry()

	skill := &Skill{
		Name:        "echo-skill",
		Description: "Echoes input",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "Echo: " + input, nil
		},
	}

	registry.Register(skill)

	// Test successful execution
	result, err := registry.Execute(context.Background(), "echo-skill", "hello")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result != "Echo: hello" {
		t.Fatalf("expected 'Echo: hello', got: %s", result)
	}

	// Test execution of non-existent skill
	_, err = registry.Execute(context.Background(), "non-existent", "hello")
	if err == nil {
		t.Fatal("expected error for non-existent skill, got nil")
	}
}

func TestSkillRegistry_Clear(t *testing.T) {
	registry := NewSkillRegistry()

	skill1 := &Skill{
		Name:        "skill-1",
		Description: "First skill",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "result1", nil
		},
	}

	skill2 := &Skill{
		Name:        "skill-2",
		Description: "Second skill",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "result2", nil
		},
	}

	registry.Register(skill1)
	registry.Register(skill2)

	if registry.Count() != 2 {
		t.Fatalf("expected 2 skills, got: %d", registry.Count())
	}

	registry.Clear()

	if registry.Count() != 0 {
		t.Fatalf("expected 0 skills after clear, got: %d", registry.Count())
	}
}

func TestSkillRegistry_Count(t *testing.T) {
	registry := NewSkillRegistry()

	if registry.Count() != 0 {
		t.Fatalf("expected count 0, got: %d", registry.Count())
	}

	skill := &Skill{
		Name:        "test-skill",
		Description: "A test skill",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "result", nil
		},
	}

	registry.Register(skill)

	if registry.Count() != 1 {
		t.Fatalf("expected count 1, got: %d", registry.Count())
	}
}

func TestSkill_HandlerExecution(t *testing.T) {
	skill := &Skill{
		Name:        "uppercase",
		Description: "Converts input to uppercase",
		Handler: func(ctx context.Context, input string) (string, error) {
			return strings.ToUpper(input), nil
		},
	}

	result, err := skill.Handler(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result != "HELLO WORLD" {
		t.Fatalf("expected 'HELLO WORLD', got: %s", result)
	}
}

func TestSkill_HandlerError(t *testing.T) {
	skill := &Skill{
		Name:        "error-skill",
		Description: "Always returns an error",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "", fmt.Errorf("intentional error")
		},
	}

	_, err := skill.Handler(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "intentional error") {
		t.Fatalf("expected 'intentional error', got: %v", err)
	}
}

func TestSkill_WithMetadata(t *testing.T) {
	metadata := map[string]interface{}{
		"version": "1.0",
		"author":  "test",
	}

	skill := &Skill{
		Name:        "meta-skill",
		Description: "Skill with metadata",
		Metadata:    metadata,
		Handler: func(ctx context.Context, input string) (string, error) {
			return "result", nil
		},
	}

	if skill.Metadata["version"] != "1.0" {
		t.Fatal("expected version metadata")
	}

	if skill.Metadata["author"] != "test" {
		t.Fatal("expected author metadata")
	}
}

func TestSkill_WithInstructions(t *testing.T) {
	instructions := "Use this skill when you need to process text data"

	skill := &Skill{
		Name:         "instructed-skill",
		Description:  "Skill with instructions",
		Instructions: instructions,
		Handler: func(ctx context.Context, input string) (string, error) {
			return "result", nil
		},
	}

	if skill.Instructions != instructions {
		t.Fatalf("expected instructions to be set, got: %s", skill.Instructions)
	}
}

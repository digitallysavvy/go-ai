package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestToolLoopAgent_WithSkills tests agent with skills integration
func TestToolLoopAgent_WithSkills(t *testing.T) {
	// Create a mock model
	mockModel := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Skill execution complete",
				FinishReason: types.FinishReasonStop,
			},
		},
	}

	// Create agent with model
	config := AgentConfig{
		Model:    mockModel,
		MaxSteps: 3,
	}
	agent := NewToolLoopAgent(config)

	// Add skills
	uppercaseSkill := &Skill{
		Name:        "uppercase",
		Description: "Converts text to uppercase",
		Handler: func(ctx context.Context, input string) (string, error) {
			return strings.ToUpper(input), nil
		},
	}

	lowercaseSkill := &Skill{
		Name:        "lowercase",
		Description: "Converts text to lowercase",
		Handler: func(ctx context.Context, input string) (string, error) {
			return strings.ToLower(input), nil
		},
	}

	err := agent.AddSkill(uppercaseSkill)
	if err != nil {
		t.Fatalf("failed to add uppercase skill: %v", err)
	}

	err = agent.AddSkill(lowercaseSkill)
	if err != nil {
		t.Fatalf("failed to add lowercase skill: %v", err)
	}

	// Verify skills are registered
	skills := agent.ListSkills()
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got: %d", len(skills))
	}

	// Execute a skill
	result, err := agent.ExecuteSkill(context.Background(), "uppercase", "hello world")
	if err != nil {
		t.Fatalf("skill execution failed: %v", err)
	}

	if result != "HELLO WORLD" {
		t.Fatalf("expected 'HELLO WORLD', got: %s", result)
	}

	// Execute agent
	agentResult, err := agent.Execute(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("agent execution failed: %v", err)
	}

	if agentResult.Text != "Skill execution complete" {
		t.Fatalf("unexpected result: %s", agentResult.Text)
	}
}

// TestToolLoopAgent_WithSubagents tests agent with subagent delegation
func TestToolLoopAgent_WithSubagents(t *testing.T) {
	// Create mock models for main and subagent
	mainModel := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Main agent response",
				FinishReason: types.FinishReasonStop,
			},
		},
	}

	researchModel := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Research findings",
				FinishReason: types.FinishReasonStop,
			},
		},
	}

	// Create main agent
	mainConfig := AgentConfig{
		Model:    mainModel,
		MaxSteps: 3,
	}
	mainAgent := NewToolLoopAgent(mainConfig)

	// Create research subagent
	researchConfig := AgentConfig{
		Model:    researchModel,
		System:   "You are a research specialist",
		MaxSteps: 5,
	}
	researchAgent := NewToolLoopAgent(researchConfig)

	// Add subagent to main agent
	err := mainAgent.AddSubagent("research", researchAgent)
	if err != nil {
		t.Fatalf("failed to add subagent: %v", err)
	}

	// Verify subagent is registered
	subagents := mainAgent.ListSubagents()
	if len(subagents) != 1 {
		t.Fatalf("expected 1 subagent, got: %d", len(subagents))
	}

	if subagents[0] != "research" {
		t.Fatalf("expected subagent named 'research', got: %s", subagents[0])
	}

	// Delegate to subagent
	result, err := mainAgent.DelegateToSubagent(context.Background(), "research", "find information")
	if err != nil {
		t.Fatalf("delegation failed: %v", err)
	}

	if result.Text != "Research findings" {
		t.Fatalf("expected 'Research findings', got: %s", result.Text)
	}

	// Execute main agent
	mainResult, err := mainAgent.Execute(context.Background(), "main task")
	if err != nil {
		t.Fatalf("main agent execution failed: %v", err)
	}

	if mainResult.Text != "Main agent response" {
		t.Fatalf("unexpected result: %s", mainResult.Text)
	}
}

// TestToolLoopAgent_SkillsAndSubagents tests both skills and subagents together
func TestToolLoopAgent_SkillsAndSubagents(t *testing.T) {
	// Create mock models
	mainModel := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Task complete",
				FinishReason: types.FinishReasonStop,
			},
		},
	}

	analysisModel := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Analysis complete",
				FinishReason: types.FinishReasonStop,
			},
		},
	}

	// Create main agent
	mainConfig := AgentConfig{
		Model:    mainModel,
		MaxSteps: 3,
	}
	mainAgent := NewToolLoopAgent(mainConfig)

	// Add skill to main agent
	dataProcessingSkill := &Skill{
		Name:        "process_data",
		Description: "Processes data",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "Processed: " + input, nil
		},
	}

	err := mainAgent.AddSkill(dataProcessingSkill)
	if err != nil {
		t.Fatalf("failed to add skill: %v", err)
	}

	// Create analysis subagent
	analysisConfig := AgentConfig{
		Model:    analysisModel,
		System:   "You are a data analysis specialist",
		MaxSteps: 5,
	}
	analysisAgent := NewToolLoopAgent(analysisConfig)

	// Add skill to analysis subagent
	statisticsSkill := &Skill{
		Name:        "calculate_stats",
		Description: "Calculates statistics",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "Statistics for: " + input, nil
		},
	}

	err = analysisAgent.AddSkill(statisticsSkill)
	if err != nil {
		t.Fatalf("failed to add skill to subagent: %v", err)
	}

	// Add subagent to main agent
	err = mainAgent.AddSubagent("analysis", analysisAgent)
	if err != nil {
		t.Fatalf("failed to add subagent: %v", err)
	}

	// Execute main agent's skill
	skillResult, err := mainAgent.ExecuteSkill(context.Background(), "process_data", "raw data")
	if err != nil {
		t.Fatalf("skill execution failed: %v", err)
	}

	if skillResult != "Processed: raw data" {
		t.Fatalf("expected 'Processed: raw data', got: %s", skillResult)
	}

	// Delegate to subagent
	delegationResult, err := mainAgent.DelegateToSubagent(context.Background(), "analysis", "analyze data")
	if err != nil {
		t.Fatalf("delegation failed: %v", err)
	}

	if delegationResult.Text != "Analysis complete" {
		t.Fatalf("expected 'Analysis complete', got: %s", delegationResult.Text)
	}

	// Execute subagent's skill directly
	subagent, exists := mainAgent.GetSubagent("analysis")
	if !exists {
		t.Fatal("subagent not found")
	}

	toolLoopSubagent, ok := subagent.(*ToolLoopAgent)
	if !ok {
		t.Fatal("subagent is not a ToolLoopAgent")
	}

	subSkillResult, err := toolLoopSubagent.ExecuteSkill(context.Background(), "calculate_stats", "dataset")
	if err != nil {
		t.Fatalf("subagent skill execution failed: %v", err)
	}

	if subSkillResult != "Statistics for: dataset" {
		t.Fatalf("expected 'Statistics for: dataset', got: %s", subSkillResult)
	}

	// Execute main agent
	result, err := mainAgent.Execute(context.Background(), "main task")
	if err != nil {
		t.Fatalf("agent execution failed: %v", err)
	}

	if result.Text != "Task complete" {
		t.Fatalf("unexpected result: %s", result.Text)
	}
}

// TestToolLoopAgent_SkillRemoval tests removing skills
func TestToolLoopAgent_SkillRemoval(t *testing.T) {
	mockModel := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Response",
				FinishReason: types.FinishReasonStop,
			},
		},
	}

	config := AgentConfig{
		Model:    mockModel,
		MaxSteps: 3,
	}
	agent := NewToolLoopAgent(config)

	// Add skill
	skill := &Skill{
		Name:        "test_skill",
		Description: "Test skill",
		Handler: func(ctx context.Context, input string) (string, error) {
			return "result", nil
		},
	}

	agent.AddSkill(skill)

	// Verify skill exists
	if !agent.config.Skills.Has("test_skill") {
		t.Fatal("skill should exist")
	}

	// Remove skill
	agent.RemoveSkill("test_skill")

	// Verify skill is removed
	if agent.config.Skills.Has("test_skill") {
		t.Fatal("skill should be removed")
	}
}

// TestToolLoopAgent_SubagentRemoval tests removing subagents
func TestToolLoopAgent_SubagentRemoval(t *testing.T) {
	mockModel := &mockLanguageModel{
		responses: []types.GenerateResult{
			{
				Text:         "Response",
				FinishReason: types.FinishReasonStop,
			},
		},
	}

	config := AgentConfig{
		Model:    mockModel,
		MaxSteps: 3,
	}
	agent := NewToolLoopAgent(config)

	// Create and add subagent
	subConfig := AgentConfig{
		Model:    mockModel,
		MaxSteps: 3,
	}
	subagent := NewToolLoopAgent(subConfig)

	agent.AddSubagent("test_subagent", subagent)

	// Verify subagent exists
	if !agent.config.Subagents.Has("test_subagent") {
		t.Fatal("subagent should exist")
	}

	// Remove subagent
	agent.RemoveSubagent("test_subagent")

	// Verify subagent is removed
	if agent.config.Subagents.Has("test_subagent") {
		t.Fatal("subagent should be removed")
	}
}

// Note: mockLanguageModel is defined in toolloop_test.go and reused here

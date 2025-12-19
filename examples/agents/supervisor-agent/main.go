package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// SubAgent represents a specialized worker agent
type SubAgent struct {
	name         string
	role         string
	model        provider.LanguageModel
	systemPrompt string
}

// SupervisorAgent manages and coordinates multiple sub-agents
type SupervisorAgent struct {
	model     provider.LanguageModel
	subAgents map[string]*SubAgent
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, _ := p.LanguageModel("gpt-4")

	// Create supervisor
	supervisor := &SupervisorAgent{
		model:     model,
		subAgents: make(map[string]*SubAgent),
	}

	// Register specialized sub-agents
	supervisor.RegisterSubAgent("researcher", &SubAgent{
		name:  "Researcher",
		role:  "research",
		model: model,
		systemPrompt: "You are a research specialist. Gather facts and information on topics. " +
			"Provide comprehensive, well-sourced information.",
	})

	supervisor.RegisterSubAgent("writer", &SubAgent{
		name:  "Writer",
		role:  "writing",
		model: model,
		systemPrompt: "You are a professional writer. Create clear, engaging, well-structured content. " +
			"Focus on clarity and readability.",
	})

	supervisor.RegisterSubAgent("reviewer", &SubAgent{
		name:  "Reviewer",
		role:  "review",
		model: model,
		systemPrompt: "You are a quality reviewer. Check content for accuracy, clarity, grammar, and style. " +
			"Provide constructive feedback and suggest improvements.",
	})

	supervisor.RegisterSubAgent("editor", &SubAgent{
		name:  "Editor",
		role:  "editing",
		model: model,
		systemPrompt: "You are an editor. Refine and polish content based on feedback. " +
			"Ensure the final output is professional and error-free.",
	})

	ctx := context.Background()

	// Example 1: Complex task requiring multiple agents
	fmt.Println("=== Example 1: Supervised Multi-Agent Workflow ===")
	task := "Create a blog post about the benefits of Go programming language"
	result := supervisor.ExecuteTask(ctx, task)
	fmt.Printf("Final Result:\n%s\n\n", result)

	// Example 2: Supervisor choosing agents dynamically
	fmt.Println("=== Example 2: Dynamic Agent Selection ===")
	task2 := "Fact-check this claim: Go is faster than Python for web servers"
	result2 := supervisor.ExecuteTask(ctx, task2)
	fmt.Printf("Final Result:\n%s\n\n", result2)
}

// RegisterSubAgent adds a sub-agent to the supervisor's roster
func (s *SupervisorAgent) RegisterSubAgent(id string, agent *SubAgent) {
	s.subAgents[id] = agent
}

// ExecuteTask analyzes the task and delegates to appropriate sub-agents
func (s *SupervisorAgent) ExecuteTask(ctx context.Context, task string) string {
	fmt.Printf("🎯 Supervisor received task: %s\n\n", task)

	// Step 1: Supervisor plans the workflow
	plan := s.planWorkflow(ctx, task)
	fmt.Printf("📋 Supervisor's Plan:\n%s\n\n", plan)

	// Step 2: Execute the plan with sub-agents
	workflow := s.parseWorkflow(plan)
	var results []string

	for i, step := range workflow {
		fmt.Printf("Step %d: %s\n", i+1, step.description)

		agent := s.subAgents[step.agentID]
		if agent == nil {
			fmt.Printf("❌ Agent '%s' not found, skipping\n\n", step.agentID)
			continue
		}

		result := agent.Execute(ctx, step.instruction)
		results = append(results, result)
		fmt.Printf("✅ %s completed\n\n", agent.name)
	}

	// Step 3: Supervisor synthesizes final output
	finalResult := s.synthesize(ctx, task, results)
	return finalResult
}

// planWorkflow asks the supervisor to create a plan
func (s *SupervisorAgent) planWorkflow(ctx context.Context, task string) string {
	// tools := []types.Tool{s.createDelegationTool()} // Optional: can use tools for delegation
	maxSteps := 1

	prompt := fmt.Sprintf(`You are a supervisor managing these sub-agents:
- researcher: Gathers information and facts
- writer: Creates content
- reviewer: Reviews and provides feedback
- editor: Refines and polishes content

Task: %s

Create a step-by-step plan. For each step, specify which agent to use and what instruction to give them.
Format: "1. [agent_id] - instruction"`, task)

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    s.model,
		Prompt:   prompt,
		MaxSteps: &maxSteps,
	})

	if err != nil {
		return fmt.Sprintf("Error planning: %v", err)
	}

	return result.Text
}

// WorkflowStep represents a single step in the workflow
type WorkflowStep struct {
	agentID     string
	description string
	instruction string
}

// parseWorkflow extracts steps from the plan
func (s *SupervisorAgent) parseWorkflow(plan string) []WorkflowStep {
	lines := strings.Split(plan, "\n")
	var steps []WorkflowStep

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse format: "1. [researcher] - gather info about Go"
		if strings.Contains(line, "[") && strings.Contains(line, "]") {
			start := strings.Index(line, "[")
			end := strings.Index(line, "]")
			if start < end {
				agentID := line[start+1 : end]
				rest := line[end+1:]
				if idx := strings.Index(rest, "-"); idx > 0 {
					instruction := strings.TrimSpace(rest[idx+1:])
					steps = append(steps, WorkflowStep{
						agentID:     agentID,
						description: line,
						instruction: instruction,
					})
				}
			}
		}
	}

	return steps
}

// Execute runs a task with this sub-agent
func (a *SubAgent) Execute(ctx context.Context, instruction string) string {
	fmt.Printf("  🤖 %s working on: %s\n", a.name, instruction)

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  a.model,
		Prompt: a.systemPrompt + "\n\nTask: " + instruction,
	})

	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	// Truncate for display
	text := result.Text
	if len(text) > 200 {
		text = text[:200] + "..."
	}

	fmt.Printf("  📝 Result: %s\n", text)
	return result.Text
}

// synthesize combines all results into final output
func (s *SupervisorAgent) synthesize(ctx context.Context, task string, results []string) string {
	prompt := fmt.Sprintf(`Task: %s

Sub-agent results:
%s

Synthesize these results into a cohesive final output.`, task, strings.Join(results, "\n\n---\n\n"))

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  s.model,
		Prompt: prompt,
	})

	if err != nil {
		return fmt.Sprintf("Error synthesizing: %v", err)
	}

	return result.Text
}

// createDelegationTool creates a tool for delegating to sub-agents
func (s *SupervisorAgent) createDelegationTool() types.Tool {
	return types.Tool{
		Name:        "delegate_to_agent",
		Description: "Delegate a task to a specialized sub-agent",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"agent": map[string]interface{}{
					"type": "string",
					"enum": []string{"researcher", "writer", "reviewer", "editor"},
				},
				"task": map[string]interface{}{
					"type":        "string",
					"description": "The task to delegate",
				},
			},
			"required": []string{"agent", "task"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			agentID := params["agent"].(string)
			task := params["task"].(string)

			agent := s.subAgents[agentID]
			if agent == nil {
				return nil, fmt.Errorf("agent not found: %s", agentID)
			}

			return agent.Execute(ctx, task), nil
		},
	}
}

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable not set")
	}

	// Create OpenAI provider
	p := openai.New(openai.Config{
		APIKey: apiKey,
	})

	// Create models for different agents
	mainModel, err := p.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatalf("Failed to create main model: %v", err)
	}

	researchModel, err := p.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatalf("Failed to create research model: %v", err)
	}

	analysisModel, err := p.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatalf("Failed to create analysis model: %v", err)
	}

	// Create main agent
	mainConfig := agent.AgentConfig{
		Model:    mainModel,
		System:   "You are a main coordinator agent that can delegate tasks to specialized subagents.",
		MaxSteps: 5,
	}
	mainAgent := agent.NewToolLoopAgent(mainConfig)

	// Create research subagent
	researchConfig := agent.AgentConfig{
		Model:    researchModel,
		System:   "You are a research specialist. Your task is to find and summarize information on requested topics.",
		MaxSteps: 3,
	}
	researchAgent := agent.NewToolLoopAgent(researchConfig)

	// Add skills to research agent
	searchSkill := &agent.Skill{
		Name:        "search",
		Description: "Simulates searching for information",
		Handler: func(ctx context.Context, input string) (string, error) {
			return fmt.Sprintf("Search results for '%s': [simulated results]", input), nil
		},
	}
	researchAgent.AddSkill(searchSkill)

	// Create analysis subagent
	analysisConfig := agent.AgentConfig{
		Model:    analysisModel,
		System:   "You are a data analysis specialist. Your task is to analyze data and provide insights.",
		MaxSteps: 3,
	}
	analysisAgent := agent.NewToolLoopAgent(analysisConfig)

	// Add skills to analysis agent
	statsSkill := &agent.Skill{
		Name:        "calculate_statistics",
		Description: "Calculates basic statistics",
		Handler: func(ctx context.Context, input string) (string, error) {
			words := strings.Fields(input)
			return fmt.Sprintf("Statistics: %d words, %d characters", len(words), len(input)), nil
		},
	}
	analysisAgent.AddSkill(statsSkill)

	// Register subagents with main agent
	if err := mainAgent.AddSubagent("research", researchAgent); err != nil {
		log.Fatalf("Failed to add research subagent: %v", err)
	}

	if err := mainAgent.AddSubagent("analysis", analysisAgent); err != nil {
		log.Fatalf("Failed to add analysis subagent: %v", err)
	}

	// List registered subagents
	fmt.Println("Main agent's subagents:")
	for _, name := range mainAgent.ListSubagents() {
		fmt.Printf("  - %s\n", name)
	}
	fmt.Println()

	// Example 1: Delegate to research subagent
	fmt.Println("=== Example 1: Research Delegation ===")
	researchResult, err := mainAgent.DelegateToSubagent(
		context.Background(),
		"research",
		"Find information about Go programming language",
	)
	if err != nil {
		log.Fatalf("Research delegation failed: %v", err)
	}
	fmt.Printf("Research result: %s\n\n", researchResult.Text)

	// Example 2: Delegate to analysis subagent
	fmt.Println("=== Example 2: Analysis Delegation ===")
	analysisResult, err := mainAgent.DelegateToSubagent(
		context.Background(),
		"analysis",
		"Analyze the performance metrics of our application",
	)
	if err != nil {
		log.Fatalf("Analysis delegation failed: %v", err)
	}
	fmt.Printf("Analysis result: %s\n\n", analysisResult.Text)

	// Example 3: Get subagent and use its skills directly
	fmt.Println("=== Example 3: Direct Subagent Skill Execution ===")
	subagent, exists := mainAgent.GetSubagent("research")
	if !exists {
		log.Fatal("Research subagent not found")
	}

	// Cast to ToolLoopAgent to access skills
	researchToolLoopAgent, ok := subagent.(*agent.ToolLoopAgent)
	if !ok {
		log.Fatal("Subagent is not a ToolLoopAgent")
	}

	skillResult, err := researchToolLoopAgent.ExecuteSkill(
		context.Background(),
		"search",
		"artificial intelligence trends",
	)
	if err != nil {
		log.Fatalf("Skill execution failed: %v", err)
	}
	fmt.Printf("Skill result: %s\n\n", skillResult)

	// Example 4: Hierarchical delegation (subagent with subagents)
	fmt.Println("=== Example 4: Hierarchical Delegation ===")

	// Create a specialized subagent for the research agent
	deepResearchConfig := agent.AgentConfig{
		Model:    researchModel,
		System:   "You are a deep research specialist focusing on technical details.",
		MaxSteps: 2,
	}
	deepResearchAgent := agent.NewToolLoopAgent(deepResearchConfig)

	// Add the deep research agent as a subagent to the research agent
	if err := researchToolLoopAgent.AddSubagent("deep_research", deepResearchAgent); err != nil {
		log.Fatalf("Failed to add deep research subagent: %v", err)
	}

	fmt.Printf("Research agent now has %d subagent(s)\n", len(researchToolLoopAgent.ListSubagents()))

	// Delegate through the hierarchy
	deepResult, err := researchToolLoopAgent.DelegateToSubagent(
		context.Background(),
		"deep_research",
		"Investigate advanced Go concurrency patterns",
	)
	if err != nil {
		log.Fatalf("Deep research delegation failed: %v", err)
	}
	fmt.Printf("Deep research result: %s\n\n", deepResult.Text)

	// Example 5: Remove and manage subagents
	fmt.Println("=== Example 5: Subagent Management ===")
	fmt.Printf("Main agent subagents before removal: %d\n", len(mainAgent.ListSubagents()))

	mainAgent.RemoveSubagent("analysis")
	fmt.Printf("Main agent subagents after removing 'analysis': %d\n", len(mainAgent.ListSubagents()))

	// Verify removal
	_, exists = mainAgent.GetSubagent("analysis")
	fmt.Printf("Analysis subagent still exists: %v\n", exists)

	// Example 6: Execute main agent with subagents available
	fmt.Println("\n=== Example 6: Main Agent Execution ===")
	mainResult, err := mainAgent.Execute(
		context.Background(),
		"Coordinate research on cloud computing trends",
	)
	if err != nil {
		log.Fatalf("Main agent execution failed: %v", err)
	}
	fmt.Printf("Main agent result: %s\n", mainResult.Text)
	fmt.Printf("Steps taken: %d\n", len(mainResult.Steps))
	fmt.Printf("Total tokens used: %v\n", mainResult.Usage.TotalTokens)

	fmt.Println("\n✅ Agent subagents example completed successfully!")
}

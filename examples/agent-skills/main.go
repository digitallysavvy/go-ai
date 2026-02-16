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

	// Create OpenAI model
	p := openai.New(openai.Config{
		APIKey: apiKey,
	})

	model, err := p.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	// Create agent
	config := agent.AgentConfig{
		Model:    model,
		System:   "You are a helpful assistant with text processing skills.",
		MaxSteps: 5,
	}
	agentInstance := agent.NewToolLoopAgent(config)

	// Define text processing skills
	uppercaseSkill := &agent.Skill{
		Name:        "uppercase",
		Description: "Converts text to uppercase",
		Instructions: "Use this skill when you need to convert text to all uppercase letters",
		Handler: func(ctx context.Context, input string) (string, error) {
			return strings.ToUpper(input), nil
		},
	}

	lowercaseSkill := &agent.Skill{
		Name:        "lowercase",
		Description: "Converts text to lowercase",
		Instructions: "Use this skill when you need to convert text to all lowercase letters",
		Handler: func(ctx context.Context, input string) (string, error) {
			return strings.ToLower(input), nil
		},
	}

	reverseSkill := &agent.Skill{
		Name:        "reverse",
		Description: "Reverses the text",
		Instructions: "Use this skill when you need to reverse the order of characters in text",
		Handler: func(ctx context.Context, input string) (string, error) {
			runes := []rune(input)
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			return string(runes), nil
		},
	}

	wordCountSkill := &agent.Skill{
		Name:        "word_count",
		Description: "Counts the number of words in text",
		Instructions: "Use this skill when you need to count how many words are in a piece of text",
		Handler: func(ctx context.Context, input string) (string, error) {
			words := strings.Fields(input)
			return fmt.Sprintf("Word count: %d", len(words)), nil
		},
		Metadata: map[string]interface{}{
			"category": "text-analysis",
			"version":  "1.0",
		},
	}

	// Add skills to agent
	if err := agentInstance.AddSkill(uppercaseSkill); err != nil {
		log.Fatalf("Failed to add uppercase skill: %v", err)
	}

	if err := agentInstance.AddSkill(lowercaseSkill); err != nil {
		log.Fatalf("Failed to add lowercase skill: %v", err)
	}

	if err := agentInstance.AddSkill(reverseSkill); err != nil {
		log.Fatalf("Failed to add reverse skill: %v", err)
	}

	if err := agentInstance.AddSkill(wordCountSkill); err != nil {
		log.Fatalf("Failed to add word_count skill: %v", err)
	}

	// List registered skills
	fmt.Println("Registered skills:")
	for _, skill := range agentInstance.ListSkills() {
		fmt.Printf("  - %s: %s\n", skill.Name, skill.Description)
	}
	fmt.Println()

	// Example 1: Execute a skill directly
	fmt.Println("=== Example 1: Direct Skill Execution ===")
	result, err := agentInstance.ExecuteSkill(context.Background(), "uppercase", "hello world")
	if err != nil {
		log.Fatalf("Failed to execute skill: %v", err)
	}
	fmt.Printf("Uppercase result: %s\n\n", result)

	// Example 2: Execute multiple skills
	fmt.Println("=== Example 2: Multiple Skills ===")
	text := "The Quick Brown Fox"

	lowercaseResult, _ := agentInstance.ExecuteSkill(context.Background(), "lowercase", text)
	fmt.Printf("Original: %s\n", text)
	fmt.Printf("Lowercase: %s\n", lowercaseResult)

	reverseResult, _ := agentInstance.ExecuteSkill(context.Background(), "reverse", text)
	fmt.Printf("Reversed: %s\n", reverseResult)

	wordCountResult, _ := agentInstance.ExecuteSkill(context.Background(), "word_count", text)
	fmt.Printf("%s\n\n", wordCountResult)

	// Example 3: Get skill information
	fmt.Println("=== Example 3: Skill Information ===")
	skill, exists := agentInstance.GetSkill("word_count")
	if exists {
		fmt.Printf("Skill: %s\n", skill.Name)
		fmt.Printf("Description: %s\n", skill.Description)
		fmt.Printf("Instructions: %s\n", skill.Instructions)
		if skill.Metadata != nil {
			fmt.Printf("Metadata: %v\n", skill.Metadata)
		}
	}
	fmt.Println()

	// Example 4: Remove and re-add a skill
	fmt.Println("=== Example 4: Skill Management ===")
	fmt.Printf("Skills before removal: %d\n", len(agentInstance.ListSkills()))

	agentInstance.RemoveSkill("reverse")
	fmt.Printf("Skills after removing 'reverse': %d\n", len(agentInstance.ListSkills()))

	// Re-add with updated handler
	enhancedReverseSkill := &agent.Skill{
		Name:        "reverse",
		Description: "Reverses text with uppercase conversion",
		Handler: func(ctx context.Context, input string) (string, error) {
			runes := []rune(strings.ToUpper(input))
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			return string(runes), nil
		},
	}
	agentInstance.AddSkill(enhancedReverseSkill)
	fmt.Printf("Skills after re-adding enhanced 'reverse': %d\n", len(agentInstance.ListSkills()))

	enhancedResult, _ := agentInstance.ExecuteSkill(context.Background(), "reverse", "hello")
	fmt.Printf("Enhanced reverse result: %s\n", enhancedResult)

	fmt.Println("\n✅ Agent skills example completed successfully!")
}

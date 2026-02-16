package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// This example demonstrates the LangChain-style callbacks introduced in v6.0.60+
// These callbacks provide fine-grained control over agent execution and align
// with LangChain's callback system for better interoperability.

func main() {
	ctx := context.Background()

	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Create OpenAI provider
	provider := openai.New(openai.Config{
		APIKey: apiKey,
	})
	model, err := provider.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	// Define tools
	weatherTool := types.Tool{
		Name:        "get_weather",
		Description: "Get the current weather for a location",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "The city and state, e.g. San Francisco, CA",
				},
			},
			"required": []string{"location"},
		},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			location := args["location"].(string)
			// Simulate API call
			time.Sleep(100 * time.Millisecond)
			return map[string]interface{}{
				"location":    location,
				"temperature": 72,
				"condition":   "sunny",
				"humidity":    45,
			}, nil
		},
	}

	calculatorTool := types.Tool{
		Name:        "calculator",
		Description: "Perform mathematical calculations",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"expression": map[string]interface{}{
					"type":        "string",
					"description": "The mathematical expression to evaluate",
				},
			},
			"required": []string{"expression"},
		},
		Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			expression := args["expression"].(string)
			// Simple simulation - in real app, use proper math parser
			return fmt.Sprintf("Result of %s = 42", expression), nil
		},
	}

	// Track execution state for demonstration
	var chainStartTime time.Time
	actionsCount := 0
	toolCallsCount := 0
	toolErrorsCount := 0

	// Create agent with all LangChain-style callbacks
	myAgent := agent.NewToolLoopAgent(agent.AgentConfig{
		Model:  model,
		System: "You are a helpful assistant with access to weather and calculator tools. Be concise.",
		Tools: []types.Tool{
			weatherTool,
			calculatorTool,
		},
		MaxSteps: 5,

		// =================================================================
		// Chain Lifecycle Callbacks
		// =================================================================

		OnChainStart: func(input string, messages []types.Message) {
			chainStartTime = time.Now()
			fmt.Println("\n" + strings.Repeat("═", 60))
			fmt.Println("🚀 CHAIN START")
			fmt.Println(strings.Repeat("═", 60))
			fmt.Printf("Input: %s\n", input)
			fmt.Printf("Messages: %d\n", len(messages))
			fmt.Printf("Started at: %s\n", chainStartTime.Format(time.RFC3339))
			fmt.Println(strings.Repeat("─", 60))
		},

		OnChainEnd: func(result *agent.AgentResult) {
			duration := time.Since(chainStartTime)
			fmt.Println("\n" + strings.Repeat("═", 60))
			fmt.Println("✅ CHAIN END (Success)")
			fmt.Println(strings.Repeat("═", 60))
			fmt.Printf("Duration: %s\n", duration)
			fmt.Printf("Total steps: %d\n", len(result.Steps))
			fmt.Printf("Total tool calls: %d\n", len(result.ToolResults))
			fmt.Printf("Total tokens: %d\n", result.Usage.GetTotalTokens())
			fmt.Printf("Finish reason: %s\n", result.FinishReason)
			if len(result.Warnings) > 0 {
				fmt.Printf("Warnings: %d\n", len(result.Warnings))
			}
			fmt.Println(strings.Repeat("═", 60))
		},

		OnChainError: func(err error) {
			duration := time.Since(chainStartTime)
			fmt.Println("\n" + strings.Repeat("═", 60))
			fmt.Println("❌ CHAIN ERROR")
			fmt.Println(strings.Repeat("═", 60))
			fmt.Printf("Duration: %s\n", duration)
			fmt.Printf("Error: %v\n", err)
			fmt.Println(strings.Repeat("═", 60))
		},

		// =================================================================
		// Agent Decision Callbacks
		// =================================================================

		OnAgentAction: func(action agent.AgentAction) {
			actionsCount++
			fmt.Println("\n" + strings.Repeat("─", 60))
			fmt.Printf("🤖 AGENT ACTION #%d (Step %d)\n", actionsCount, action.StepNumber)
			fmt.Println(strings.Repeat("─", 60))
			fmt.Printf("Tool: %s\n", action.ToolCall.ToolName)
			fmt.Printf("Tool Call ID: %s\n", action.ToolCall.ID)
			if action.Reasoning != "" {
				fmt.Printf("Reasoning: %s\n", action.Reasoning)
			}
			fmt.Printf("Arguments: %v\n", action.ToolCall.Arguments)
			// Run tracking (v6.0.61+)
			if action.RunID != "" {
				fmt.Printf("Run ID: %s\n", action.RunID)
			}
			if action.ParentRunID != "" {
				fmt.Printf("Parent Run ID: %s\n", action.ParentRunID)
			}
			if len(action.Tags) > 0 {
				fmt.Printf("Tags: %v\n", action.Tags)
			}
			fmt.Println(strings.Repeat("─", 60))
		},

		OnAgentFinish: func(finish agent.AgentFinish) {
			fmt.Println("\n" + strings.Repeat("─", 60))
			fmt.Printf("🏁 AGENT FINISH (Step %d)\n", finish.StepNumber)
			fmt.Println(strings.Repeat("─", 60))
			fmt.Printf("Output: %s\n", finish.Output)
			fmt.Printf("Finish Reason: %s\n", finish.FinishReason)
			if metadata, ok := finish.Metadata["total_steps"].(int); ok {
				fmt.Printf("Total Steps: %d\n", metadata)
			}
			if metadata, ok := finish.Metadata["max_steps_hit"].(bool); ok && metadata {
				fmt.Println("⚠️  Max steps reached")
			}
			// Run tracking (v6.0.61+)
			if finish.RunID != "" {
				fmt.Printf("Run ID: %s\n", finish.RunID)
			}
			if finish.ParentRunID != "" {
				fmt.Printf("Parent Run ID: %s\n", finish.ParentRunID)
			}
			if len(finish.Tags) > 0 {
				fmt.Printf("Tags: %v\n", finish.Tags)
			}
			fmt.Println(strings.Repeat("─", 60))
		},

		// =================================================================
		// Tool Lifecycle Callbacks
		// =================================================================

		OnToolStart: func(toolCall types.ToolCall) {
			toolCallsCount++
			fmt.Println()
			fmt.Printf("  🔧 Tool Start: %s (call #%d)\n", toolCall.ToolName, toolCallsCount)
			fmt.Printf("     ID: %s\n", toolCall.ID)
		},

		OnToolEnd: func(toolResult types.ToolResult) {
			fmt.Printf("  ✅ Tool End: %s\n", toolResult.ToolName)
			if toolResult.ProviderExecuted {
				fmt.Println("     (Provider-executed)")
			} else {
				fmt.Printf("     Result: %v\n", toolResult.Result)
			}
		},

		OnToolError: func(toolCall types.ToolCall, err error) {
			toolErrorsCount++
			fmt.Printf("  ❌ Tool Error: %s\n", toolCall.ToolName)
			fmt.Printf("     Error: %v\n", err)
		},

		// =================================================================
		// Legacy Callbacks (still supported)
		// =================================================================

		OnStepStart: func(stepNum int) {
			fmt.Printf("\n>>> Step %d starting...\n", stepNum)
		},

		OnStepFinish: func(step types.StepResult) {
			fmt.Printf(">>> Step %d finished (reason: %s)\n", step.StepNumber, step.FinishReason)
		},
	})

	// Execute the agent with run tracking (v6.0.61+)
	// Add tags to categorize this execution
	ctx = agent.WithTags(ctx, []string{"demo", "langchain-callbacks", "multi-tool"})

	fmt.Println("Starting agent with LangChain-style callbacks...")
	fmt.Println("Question: What's the weather in San Francisco and what's 15 + 27?")

	result, err := myAgent.Execute(ctx, "What's the weather in San Francisco and what's 15 + 27?")

	// Summary
	fmt.Println("\n\n" + strings.Repeat("═", 60))
	fmt.Println("📊 EXECUTION SUMMARY")
	fmt.Println(strings.Repeat("═", 60))

	if err != nil {
		fmt.Printf("Status: ❌ Failed\n")
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Status: ✅ Success\n")
		fmt.Printf("Final Output: %s\n", result.Text)
	}

	fmt.Printf("\nStatistics:\n")
	fmt.Printf("  Agent Actions: %d\n", actionsCount)
	fmt.Printf("  Tool Calls: %d\n", toolCallsCount)
	fmt.Printf("  Tool Errors: %d\n", toolErrorsCount)

	if result != nil {
		fmt.Printf("  Steps: %d\n", len(result.Steps))
		fmt.Printf("  Total Tokens: %d\n", result.Usage.GetTotalTokens())

		if result.Usage.InputTokens != nil {
			fmt.Printf("    - Input: %d\n", *result.Usage.InputTokens)
		}
		if result.Usage.OutputTokens != nil {
			fmt.Printf("    - Output: %d\n", *result.Usage.OutputTokens)
		}
	}

	fmt.Println(strings.Repeat("═", 60))
}

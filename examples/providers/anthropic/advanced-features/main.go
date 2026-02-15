package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func main() {
	// Get API key from environment
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	// Create provider
	provider := anthropic.New(anthropic.Config{
		APIKey: apiKey,
	})

	fmt.Println("=== Anthropic Advanced Features Demo ===\n")

	// Demo 1: Fast Mode
	demoFastMode(provider)

	// Demo 2: Adaptive Thinking
	demoAdaptiveThinking(provider)

	// Demo 3: Extended Thinking with Budget
	demoExtendedThinking(provider)

	// Demo 4: Combined Features
	demoCombinedFeatures(provider)
}

// demoFastMode demonstrates fast mode for rapid responses
func demoFastMode(provider *anthropic.Provider) {
	fmt.Println("--- Demo 1: Fast Mode ---")
	fmt.Println("Fast mode provides 2.5x faster output speeds (Opus 4.6 only)\n")

	// Create model with fast mode
	options := &anthropic.ModelOptions{
		Speed: anthropic.SpeedFast,
	}

	model, err := provider.LanguageModelWithOptions("claude-opus-4-6", options)
	if err != nil {
		log.Printf("Fast mode error: %v\n\n", err)
		return
	}

	// Generate text
	result, err := ai.GenerateText(context.Background(), model,
		"What is the capital of France? Answer in one word.")
	if err != nil {
		log.Printf("Error: %v\n\n", err)
		return
	}

	fmt.Printf("Response: %s\n", result.Text)
	fmt.Printf("Tokens used: %d input, %d output\n\n",
		result.Usage.InputTokens, result.Usage.OutputTokens)
}

// demoAdaptiveThinking demonstrates adaptive thinking for complex reasoning
func demoAdaptiveThinking(provider *anthropic.Provider) {
	fmt.Println("--- Demo 2: Adaptive Thinking ---")
	fmt.Println("Adaptive thinking shows Claude's reasoning process\n")

	// Create model with adaptive thinking
	options := &anthropic.ModelOptions{
		Thinking: &anthropic.ThinkingConfig{
			Type: anthropic.ThinkingTypeAdaptive,
		},
	}

	model, err := provider.LanguageModelWithOptions("claude-opus-4-6", options)
	if err != nil {
		log.Printf("Adaptive thinking error: %v\n\n", err)
		return
	}

	// Ask a logic puzzle
	prompt := `Three switches control three light bulbs in a closed room.
You can flip the switches as many times as you want, but you can only
enter the room once. How can you determine which switch controls which bulb?`

	result, err := ai.GenerateText(context.Background(), model, prompt)
	if err != nil {
		log.Printf("Error: %v\n\n", err)
		return
	}

	// Display thinking content if available
	if resp, ok := result.RawResponse.(map[string]interface{}); ok {
		if content, ok := resp["content"].([]interface{}); ok {
			for _, c := range content {
				if contentMap, ok := c.(map[string]interface{}); ok {
					if contentMap["type"] == "thinking" {
						if thinking, ok := contentMap["thinking"].(string); ok {
							fmt.Println("Claude's Thinking Process:")
							fmt.Println(thinking)
							fmt.Println()
						}
					}
				}
			}
		}
	}

	fmt.Printf("Answer: %s\n", result.Text)
	fmt.Printf("Tokens used: %d input, %d output\n\n",
		result.Usage.InputTokens, result.Usage.OutputTokens)
}

// demoExtendedThinking demonstrates extended thinking with budget
func demoExtendedThinking(provider *anthropic.Provider) {
	fmt.Println("--- Demo 3: Extended Thinking with Budget ---")
	fmt.Println("For models before Opus 4.6, specify a thinking budget\n")

	// Create model with extended thinking and budget
	budget := 3000
	options := &anthropic.ModelOptions{
		Thinking: &anthropic.ThinkingConfig{
			Type:         anthropic.ThinkingTypeEnabled,
			BudgetTokens: &budget,
		},
	}

	model, err := provider.LanguageModelWithOptions("claude-sonnet-4", options)
	if err != nil {
		log.Printf("Extended thinking error: %v\n\n", err)
		return
	}

	// Ask a mathematical problem
	prompt := `Calculate the compound interest on $10,000 invested for 5 years
at 6% annual interest, compounded quarterly. Show your work.`

	result, err := ai.GenerateText(context.Background(), model, prompt)
	if err != nil {
		log.Printf("Error: %v\n\n", err)
		return
	}

	fmt.Printf("Response: %s\n", result.Text)
	fmt.Printf("Tokens used: %d input, %d output\n",
		result.Usage.InputTokens, result.Usage.OutputTokens)
	fmt.Printf("Thinking budget: %d tokens\n\n", budget)
}

// demoCombinedFeatures demonstrates using fast mode and thinking together
func demoCombinedFeatures(provider *anthropic.Provider) {
	fmt.Println("--- Demo 4: Combined Features ---")
	fmt.Println("Fast mode + adaptive thinking for rapid, reasoned responses\n")

	// Create model with both fast mode and adaptive thinking
	options := &anthropic.ModelOptions{
		Speed: anthropic.SpeedFast,
		Thinking: &anthropic.ThinkingConfig{
			Type: anthropic.ThinkingTypeAdaptive,
		},
	}

	model, err := provider.LanguageModelWithOptions("claude-opus-4-6", options)
	if err != nil {
		log.Printf("Combined features error: %v\n\n", err)
		return
	}

	// Ask a strategic question
	prompt := `You have $1000. Should you invest in:
A) A high-risk, high-reward stock
B) A low-risk bond with guaranteed returns
C) A mix of both

Consider risk tolerance, time horizon, and diversification. Give a brief recommendation.`

	result, err := ai.GenerateText(context.Background(), model, prompt)
	if err != nil {
		log.Printf("Error: %v\n\n", err)
		return
	}

	fmt.Printf("Response: %s\n", result.Text)
	fmt.Printf("Tokens used: %d input, %d output\n",
		result.Usage.InputTokens, result.Usage.OutputTokens)
	fmt.Printf("Features: Fast mode + Adaptive thinking\n\n")
}

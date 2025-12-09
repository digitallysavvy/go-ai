package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func main() {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	p := anthropic.New(anthropic.Config{
		APIKey: apiKey,
	})

	// Use Claude Sonnet 4 or Opus 4 for extended thinking
	model, err := p.LanguageModel("claude-sonnet-4")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Example 1: Complex Problem Solving ===")
	solveComplexProblem(ctx, model)

	fmt.Println("\n=== Example 2: Multi-Step Reasoning ===")
	multiStepReasoning(ctx, model)

	fmt.Println("\n=== Example 3: Code Analysis with Thinking ===")
	analyzeCodeWithThinking(ctx, model)
}

func solveComplexProblem(ctx context.Context, model provider.LanguageModel) {
	// Extended thinking allows the model to "think" before responding
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Prompt: `Solve this problem step by step:

You have a 3-gallon jug and a 5-gallon jug. How can you measure exactly 4 gallons
of water? You have an unlimited water supply.

Think through all possible approaches and explain your reasoning process.`,
		System: "Take your time to think through the problem carefully. Show your reasoning steps.",
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("Solution with Extended Thinking:")
	fmt.Println(result.Text)
	fmt.Printf("\nToken usage: %d\n", result.Usage.TotalTokens)
	fmt.Printf("Finish reason: %s\n", result.FinishReason)
}

func multiStepReasoning(ctx context.Context, model provider.LanguageModel) {
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Prompt: `A bat and a ball cost $1.10 in total. The bat costs $1.00 more than the ball.
How much does the ball cost?

Think carefully and avoid the common mistake. Show your reasoning.`,
		System: "Use extended thinking to reason through this carefully and check your answer.",
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("Answer with Reasoning:")
	fmt.Println(result.Text)
}

func analyzeCodeWithThinking(ctx context.Context, model provider.LanguageModel) {
	code := `
func ProcessData(data []int) int {
    result := 0
    for i := 0; i < len(data); i++ {
        if data[i] > 0 {
            result += data[i]
            for j := 0; j < len(data); j++ {
                if i != j && data[j] > data[i] {
                    result -= 1
                }
            }
        }
    }
    return result
}
`

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Prompt: fmt.Sprintf(`Analyze this code and explain:
1. What does it do?
2. What is its time complexity?
3. Are there any bugs or issues?
4. How can it be optimized?

Code:
%s

Think through each aspect carefully before answering.`, code),
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("Code Analysis:")
	fmt.Println(result.Text)
	fmt.Printf("\nToken usage: %d\n", result.Usage.TotalTokens)
}

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	p := openai.New(openai.Config{
		APIKey: apiKey,
	})

	ctx := context.Background()

	fmt.Println("=== Example 1: o1-preview - Complex Math Problem ===")
	solveComplexMath(ctx, p)

	fmt.Println("\n=== Example 2: o1-mini - Logic Puzzle ===")
	solveLogicPuzzle(ctx, p)

	fmt.Println("\n=== Example 3: o1-preview - Code Optimization ===")
	optimizeCode(ctx, p)
}

func solveComplexMath(ctx context.Context, p *openai.Provider) {
	// o1 models are optimized for complex reasoning
	model, err := p.LanguageModel("o1-preview")
	if err != nil {
		log.Fatal(err)
	}

	// Note: o1 models don't support temperature, top_p, or system messages
	// They use a fixed reasoning approach
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Prompt: `Solve this problem step by step:

A train leaves Station A traveling at 60 mph. Two hours later, another train leaves Station A
traveling at 90 mph in the same direction. How long will it take the second train to catch up
to the first train? Show all your work and reasoning.`,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("Solution:")
	fmt.Println(result.Text)
	fmt.Printf("\nToken usage:\n")
	fmt.Printf("  Input tokens: %d\n", result.Usage.GetInputTokens())
	fmt.Printf("  Output tokens: %d\n", result.Usage.GetOutputTokens())

	// o1 models track reasoning tokens separately
	if result.Usage.GetTotalTokens() > result.Usage.GetInputTokens()+result.Usage.GetOutputTokens() {
		reasoningTokens := result.Usage.GetTotalTokens() - result.Usage.GetInputTokens() - result.Usage.GetOutputTokens()
		fmt.Printf("  Reasoning tokens: %d\n", reasoningTokens)
	}

	fmt.Printf("  Total tokens: %d\n", result.Usage.GetTotalTokens())
	fmt.Printf("\nFinish reason: %s\n", result.FinishReason)
}

func solveLogicPuzzle(ctx context.Context, p *openai.Provider) {
	// o1-mini is faster and more cost-effective for simpler reasoning
	model, err := p.LanguageModel("o1-mini")
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Prompt: `Solve this logic puzzle:

Five houses are in a row, each painted a different color. In each house lives a person of a
different nationality. The five owners drink different beverages, smoke different brands of
cigarettes, and keep different pets.

1. The Brit lives in the red house
2. The Swede keeps dogs
3. The Dane drinks tea
4. The green house is on the left of the white house
5. The green house owner drinks coffee
6. The person who smokes Pall Mall keeps birds
7. The owner of the yellow house smokes Dunhill
8. The man living in the center house drinks milk
9. The Norwegian lives in the first house
10. The man who smokes Blend lives next to the one who keeps cats
11. The man who keeps horses lives next to the man who smokes Dunhill
12. The owner who smokes BlueMaster drinks beer
13. The German smokes Prince
14. The Norwegian lives next to the blue house
15. The man who smokes Blend has a neighbor who drinks water

Who owns the fish?`,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("Solution:")
	fmt.Println(result.Text)
	fmt.Printf("\nToken usage: %d total\n", result.Usage.GetTotalTokens())
}

func optimizeCode(ctx context.Context, p *openai.Provider) {
	model, err := p.LanguageModel("o1-preview")
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Prompt: `Analyze and optimize this code for performance:

` + "```go" + `
func FindDuplicates(nums []int) []int {
    result := []int{}
    for i := 0; i < len(nums); i++ {
        for j := i + 1; j < len(nums); j++ {
            if nums[i] == nums[j] {
                found := false
                for _, v := range result {
                    if v == nums[i] {
                        found = true
                        break
                    }
                }
                if !found {
                    result = append(result, nums[i])
                }
            }
        }
    }
    return result
}
` + "```" + `

Provide:
1. Time complexity analysis of the current code
2. Optimized version with better time complexity
3. Explanation of the improvements
4. Test cases to verify correctness`,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	fmt.Println("Analysis and Optimization:")
	fmt.Println(result.Text)
	fmt.Printf("\nToken usage: %d total\n", result.Usage.GetTotalTokens())
}

//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// Example: Text generation with gpt-5.3-codex
// Demonstrates non-streaming text generation using the gpt-5.3-codex model.
//
// Run with: OPENAI_API_KEY=... go run gpt-5.3-codex-generate.go

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	prov := openai.New(openai.Config{APIKey: apiKey})

	model, err := prov.LanguageModel("gpt-5.3-codex")
	if err != nil {
		log.Fatalf("Failed to create language model: %v", err)
	}

	ctx := context.Background()

	// Basic code generation
	fmt.Println("=== gpt-5.3-codex: Code Generation ===")
	fmt.Println()

	result, err := model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleSystem,
					Content: []types.ContentPart{
						types.TextContent{Text: "You are an expert Go programmer. Write clean, idiomatic Go code."},
					},
				},
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "Write a Go function that checks whether a string is a valid email address using a regex pattern."},
					},
				},
			},
		},
	})
	if err != nil {
		log.Fatalf("Generation failed: %v", err)
	}

	fmt.Println(result.Text)
	fmt.Println()
	fmt.Printf("Finish reason: %s\n", result.FinishReason)
	fmt.Printf("Input tokens:  %d\n", result.Usage.GetInputTokens())
	fmt.Printf("Output tokens: %d\n", result.Usage.GetOutputTokens())
	fmt.Println()

	// Code explanation
	fmt.Println("=== gpt-5.3-codex: Code Explanation ===")
	fmt.Println()

	code := `
func fibonacci(n int) int {
    if n <= 1 {
        return n
    }
    a, b := 0, 1
    for i := 2; i <= n; i++ {
        a, b = b, a+b
    }
    return b
}`

	explainResult, err := model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: types.Prompt{
			Text: fmt.Sprintf("Explain what this Go function does and analyze its time and space complexity:\n%s", code),
		},
	})
	if err != nil {
		log.Fatalf("Explanation failed: %v", err)
	}

	fmt.Println(explainResult.Text)
	fmt.Println()
	fmt.Printf("Input tokens:  %d\n", explainResult.Usage.GetInputTokens())
	fmt.Printf("Output tokens: %d\n", explainResult.Usage.GetOutputTokens())
}

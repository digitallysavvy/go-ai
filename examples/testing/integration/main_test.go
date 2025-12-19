package main

import (
	"context"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func TestIntegrationGenerate(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, err := p.LanguageModel("gpt-4")
	if err != nil {
		t.Fatal(err)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Say 'test' once",
	})

	if err != nil {
		t.Fatalf("Generation failed: %v", err)
	}

	if result.Text == "" {
		t.Error("Expected non-empty response")
	}

	if result.Usage.TotalTokens == 0 {
		t.Error("Expected non-zero token usage")
	}

	t.Logf("Response: %s", result.Text)
	t.Logf("Tokens: %d", result.Usage.TotalTokens)
}

func TestIntegrationWithTools(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, _ := p.LanguageModel("gpt-4")

	// Define a simple tool
	calcTool := mockCalculatorTool()
	maxSteps := 3

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:    model,
		Prompt:   "What is 5 + 3?",
		Tools:    []types.Tool{calcTool},
		MaxSteps: &maxSteps,
	})

	if err != nil {
		t.Fatalf("Generation with tools failed: %v", err)
	}

	t.Logf("Response: %s", result.Text)
}

func mockCalculatorTool() types.Tool {
	return types.Tool{
		Name:        "calculator",
		Description: "A simple calculator",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type": "string",
					"enum": []string{"add", "subtract", "multiply", "divide"},
				},
				"a": map[string]interface{}{"type": "number"},
				"b": map[string]interface{}{"type": "number"},
			},
			"required": []string{"operation", "a", "b"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "8", nil // Mock result
		},
	}
}

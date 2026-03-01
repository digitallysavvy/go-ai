package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// This example demonstrates how to use structured callback events for
// observability in the Go-AI SDK.
//
// Structured callbacks fire at each lifecycle stage of generation:
//   - OnStart:           once, before any LLM request
//   - OnStepStart:       once per step (LLM call)
//   - OnToolCallStart:   once per tool, before execution
//   - OnToolCallFinish:  once per tool, after execution (success or error)
//   - OnStepFinish:      once per step, after all tools for that step run
//   - OnFinish:          once, after all steps complete
//
// Usage:
//
//	export OPENAI_API_KEY=your-api-key
//	go run main.go

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, err := p.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	// A simple calculator tool the model can use.
	calculatorTool := types.Tool{
		Name:        "calculator",
		Description: "Evaluates a basic arithmetic expression and returns the result",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"expression": map[string]interface{}{
					"type":        "string",
					"description": "Arithmetic expression to evaluate, e.g. '(3 + 5) * 2'",
				},
			},
			"required": []string{"expression"},
		},
		Execute: func(_ context.Context, args map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			expr, _ := args["expression"].(string)
			// Stub: just echo the expression with a fake result for demonstration.
			return fmt.Sprintf("Result of %q = 42 (stub)", expr), nil
		},
	}

	// A simple request logger that prints structured event details to stdout.
	// In production you would replace these log.Printf calls with your
	// metrics library, tracing SDK (OpenTelemetry, Datadog, etc.), or
	// structured logger (zap, slog, etc.).
	start := time.Now()

	ctx := context.Background()

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "What is (12 + 8) multiplied by 3? Use the calculator tool.",
		Tools:  []types.Tool{calculatorTool},

		// ── Lifecycle callbacks ─────────────────────────────────────────────

		OnStart: func(_ context.Context, e ai.OnStartEvent) {
			log.Printf("[OnStart] provider=%s model=%s prompt=%q tools=%d",
				e.ModelProvider, e.ModelID, e.Prompt, len(e.Tools))
		},

		OnStepStart: func(_ context.Context, e ai.OnStepStartEvent) {
			log.Printf("[OnStepStart] step=%d messages=%d",
				e.StepNumber, len(e.Messages))
		},

		OnToolCallStart: func(_ context.Context, e ai.OnToolCallStartEvent) {
			log.Printf("[OnToolCallStart] step=%d tool=%s id=%s args=%v",
				e.StepNumber, e.ToolName, e.ToolCallID, e.Args)
		},

		OnToolCallFinish: func(_ context.Context, e ai.OnToolCallFinishEvent) {
			if e.Error != nil {
				log.Printf("[OnToolCallFinish] step=%d tool=%s ERROR: %v",
					e.StepNumber, e.ToolName, e.Error)
				return
			}
			log.Printf("[OnToolCallFinish] step=%d tool=%s result=%v",
				e.StepNumber, e.ToolName, e.Result)
		},

		OnStepFinishEvent: func(_ context.Context, e ai.OnStepFinishEvent) {
			inputTok := int64(0)
			if e.Usage.InputTokens != nil {
				inputTok = *e.Usage.InputTokens
			}
			outputTok := int64(0)
			if e.Usage.OutputTokens != nil {
				outputTok = *e.Usage.OutputTokens
			}
			log.Printf("[OnStepFinish] step=%d finish_reason=%s tool_calls=%d input_tokens=%d output_tokens=%d",
				e.StepNumber, e.FinishReason, len(e.ToolCalls), inputTok, outputTok)
		},

		OnFinishEvent: func(_ context.Context, e ai.OnFinishEvent) {
			totalTok := int64(0)
			if e.TotalUsage.TotalTokens != nil {
				totalTok = *e.TotalUsage.TotalTokens
			}
			elapsed := time.Since(start)
			log.Printf("[OnFinish] steps=%d total_tokens=%d finish_reason=%s elapsed=%s",
				len(e.Steps), totalTok, e.FinishReason, elapsed.Round(time.Millisecond))
		},
	})
	if err != nil {
		log.Fatalf("GenerateText error: %v", err)
	}

	fmt.Printf("\nFinal answer: %s\n", result.Text)
}

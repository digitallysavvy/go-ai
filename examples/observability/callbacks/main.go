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
//   - OnToolExecutionStart: once per tool, before execution
//   - OnToolExecutionEnd:   once per tool, after execution (success or error)
//   - OnStepEnd:         once per step, after all tools for that step run
//   - OnEnd:             once, after all steps complete
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
	// currentStep tracks the step number for the tool-execution callbacks:
	// OnToolCallStartEvent/OnToolCallFinishEvent no longer carry StepNumber
	// (removed from the TypeScript event), so it's correlated here from the
	// most recent OnStepStart instead.
	currentStep := 0

	ctx := context.Background()

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:    model,
		Prompt:   "What is (12 + 8) multiplied by 3? Use the calculator tool.",
		Tools:    []types.Tool{calculatorTool},
		StopWhen: []ai.StopCondition{ai.IsStepCount(5)},

		// ── Lifecycle callbacks ─────────────────────────────────────────────

		OnStart: func(_ context.Context, e ai.OnStartEvent) {
			log.Printf("[OnStart] provider=%s model=%s prompt=%q tools=%d",
				e.Provider, e.ModelID, e.Prompt, len(e.Tools))
		},

		OnStepStart: func(_ context.Context, e ai.OnStepStartEvent) {
			currentStep = e.StepNumber
			log.Printf("[OnStepStart] step=%d messages=%d",
				e.StepNumber, len(e.Messages))
		},

		OnToolExecutionStart: func(_ context.Context, e ai.OnToolCallStartEvent) {
			log.Printf("[OnToolExecutionStart] step=%d tool=%s id=%s args=%v",
				currentStep, e.ToolName, e.ToolCallID, e.ToolCall.Arguments)
		},

		OnToolExecutionEnd: func(_ context.Context, e ai.OnToolCallFinishEvent) {
			if e.ToolOutput.Error != nil {
				log.Printf("[OnToolExecutionEnd] step=%d tool=%s ERROR: %v",
					currentStep, e.ToolName, e.ToolOutput.Error)
				return
			}
			log.Printf("[OnToolExecutionEnd] step=%d tool=%s result=%v",
				currentStep, e.ToolName, e.ToolOutput.Result)
		},

		OnStepEndEvent: func(_ context.Context, e ai.OnStepFinishEvent) {
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

		OnEndEvent: func(_ context.Context, e ai.OnFinishEvent) {
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

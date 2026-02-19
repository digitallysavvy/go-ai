//go:build ignore

// stop-when demonstrates three ways to control when a tool-calling loop stops:
//
//  1. StepCountIs — hard ceiling on the number of steps
//  2. HasToolCall  — stop when the model calls a named "finish" tool
//  3. Custom closure — stop when a token budget is exceeded
//
// It also shows how to read result.StopReason to understand why the loop ended.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, err := p.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Pattern 1: StepCountIs ===")
	runWithStepCount(ctx, model)

	fmt.Println("\n=== Pattern 2: HasToolCall ===")
	runWithHasToolCall(ctx, model)

	fmt.Println("\n=== Pattern 3: Token-budget closure ===")
	runWithTokenBudget(ctx, model)
}

// runWithStepCount stops the loop after at most 3 tool-calling steps.
func runWithStepCount(ctx context.Context, model provider.LanguageModel) {
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Search for information about Go programming, then summarize what you found.",
		Tools:  []types.Tool{makeSearchTool()},
		StopWhen: []ai.StopCondition{
			ai.StepCountIs(3), // stop after 3 tool-calling rounds
		},
	})
	if err != nil {
		log.Printf("error: %v", err)
		return
	}

	fmt.Printf("Steps: %d\n", len(result.Steps))
	fmt.Printf("StopReason: %q\n", result.StopReason)
	fmt.Printf("Answer: %s\n", result.Text)
}

// runWithHasToolCall stops the loop when the model calls the "finish" tool,
// which it uses to signal that it has completed its task.
func runWithHasToolCall(ctx context.Context, model provider.LanguageModel) {
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model: model,
		Prompt: `Search for information about the Go language.
When you are satisfied with your research, call the finish tool with your conclusion.`,
		Tools: []types.Tool{
			makeSearchTool(),
			makeFinishTool(),
		},
		StopWhen: []ai.StopCondition{
			ai.HasToolCall("finish"), // stop when model signals completion
			ai.StepCountIs(10),       // safety ceiling in case model never calls finish
		},
	})
	if err != nil {
		log.Printf("error: %v", err)
		return
	}

	fmt.Printf("Steps: %d\n", len(result.Steps))
	fmt.Printf("StopReason: %q\n", result.StopReason)
	fmt.Printf("Answer: %s\n", result.Text)
}

// runWithTokenBudget adds a custom token-budget condition placed BEFORE StepCountIs.
//
// Because EvaluateStopConditions runs ALL conditions before returning the first
// match, the token-budget closure always executes even when StepCountIs would
// also trigger. This guarantees any side effects (alerting, metric recording)
// run regardless of ordering.
//
// Place side-effectful or priority conditions BEFORE safety ceilings to make
// the intent explicit and to ensure they are not silently skipped.
func runWithTokenBudget(ctx context.Context, model provider.LanguageModel) {
	const budgetTokens = int64(2000)

	tokenBudget := func(state ai.StopConditionState) string {
		if state.Usage.TotalTokens != nil && *state.Usage.TotalTokens > budgetTokens {
			return fmt.Sprintf("token budget exceeded (%d tokens)", *state.Usage.TotalTokens)
		}
		return ""
	}

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Perform a thorough multi-step analysis of Go concurrency patterns.",
		Tools:  []types.Tool{makeSearchTool()},
		StopWhen: []ai.StopCondition{
			tokenBudget,        // side-effectful condition comes first
			ai.StepCountIs(10), // safety ceiling last
		},
	})
	if err != nil {
		log.Printf("error: %v", err)
		return
	}

	fmt.Printf("Steps: %d\n", len(result.Steps))
	if result.Usage.TotalTokens != nil {
		fmt.Printf("Total tokens: %d\n", *result.Usage.TotalTokens)
	}
	fmt.Printf("StopReason: %q\n", result.StopReason)
	fmt.Printf("Answer: %s\n", result.Text)
}

func makeSearchTool() types.Tool {
	return types.Tool{
		Name:        "search",
		Description: "Search the web for information on a topic",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query",
				},
			},
			"required": []string{"query"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			query := params["query"].(string)
			return map[string]interface{}{
				"query":   query,
				"results": "Go is a statically typed, compiled language designed at Google.",
			}, nil
		},
	}
}

func makeFinishTool() types.Tool {
	return types.Tool{
		Name:        "finish",
		Description: "Signal that research is complete and provide a conclusion",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"conclusion": map[string]interface{}{
					"type":        "string",
					"description": "Final conclusion from the research",
				},
			},
			"required": []string{"conclusion"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			conclusion := params["conclusion"].(string)
			return map[string]interface{}{
				"status":     "complete",
				"conclusion": conclusion,
			}, nil
		},
	}
}

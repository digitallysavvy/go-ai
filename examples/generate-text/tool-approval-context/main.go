package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, err := p.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatalf("failed to create model: %v", err)
	}

	deleteAccount := types.Tool{
		Name:          "delete_account",
		Description:   "Delete an account by ID.",
		Parameters:    schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
		ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{
				"deleted":      false,
				"runtime_user": opts.RuntimeContext,
				"tool_policy":  opts.ToolContext,
			}, nil
		},
	}

	denyReason := "destructive account changes require a human approval workflow"
	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Delete account acct_123.",
		Tools:  []types.Tool{deleteAccount},
		RuntimeContext: map[string]interface{}{
			"user_id": "user_42",
		},
		ToolsContext: map[string]interface{}{
			"delete_account": map[string]interface{}{
				"environment": "production",
			},
		},
		ToolApproval: map[string]ai.ToolApprovalValue{
			"delete_account": ai.ToolApprovalResult{
				Status: ai.ToolApprovalStatusDenied,
				Reason: &denyReason,
			},
		},
		StopWhen: []ai.StopCondition{ai.IsStepCount(3)},
	})
	if err != nil {
		log.Fatalf("generation failed: %v", err)
	}

	fmt.Println(result.Text)
	for _, toolResult := range result.ToolResults {
		fmt.Printf("%s: %v\n", toolResult.ToolName, toolResult.Result)
	}
}

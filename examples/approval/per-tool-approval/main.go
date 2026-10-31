//go:build ignore

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
	p := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})
	model, err := p.LanguageModel(openai.ModelGPT4oMini)
	if err != nil {
		log.Fatal(err)
	}

	tools := []types.Tool{
		makeTool("read_profile"),
		makeTool("delete_account"),
		makeTool("send_invoice"),
	}
	denyReason := "account deletion is blocked in this environment"
	reviewReason := "invoice sending requires human approval"

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Read the profile, delete the account, and send an invoice.",
		Tools:  tools,
		ToolApproval: map[string]ai.ToolApprovalValue{
			"read_profile":   ai.ToolApprovalStatusApproved,
			"delete_account": ai.ToolApprovalResult{Status: ai.ToolApprovalStatusDenied, Reason: &denyReason},
			"send_invoice":   ai.ToolApprovalResult{Status: ai.ToolApprovalStatusUserApproval, Reason: &reviewReason},
		},
		StopWhen: []ai.StopCondition{ai.IsStepCount(3)},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(result.Text)
	// Executed calls are in ToolResults (as in the TS SDK); approval requests
	// and responses are content parts.
	for _, toolResult := range result.ToolResults {
		fmt.Printf("%s result=%v\n", toolResult.ToolName, toolResult.Result)
	}
	for _, part := range result.Content {
		switch p := part.(type) {
		case types.ToolApprovalRequestContent:
			fmt.Printf("%s approval requested (%s)\n", p.ToolCall.ToolName, p.ApprovalID)
		case types.ToolApprovalResponseContent:
			fmt.Printf("%s approved=%v\n", p.ToolCall.ToolName, p.Approved)
		}
	}
}

func makeTool(name string) types.Tool {
	return types.Tool{
		Name:        name,
		Description: "Example tool " + name,
		Parameters:  schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"ok": true, "toolCallId": opts.ToolCallID}, nil
		},
	}
}

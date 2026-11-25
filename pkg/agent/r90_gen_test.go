package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// r90genTwoStepModel answers the first call with calls and every later call
// with the text "done".
func r90genTwoStepModel(calls []types.ToolCall) *functionalAgentLanguageModel {
	n := 0
	return &functionalAgentLanguageModel{doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		n++
		if n == 1 {
			return &types.GenerateResult{ToolCalls: calls, FinishReason: types.FinishReasonToolCalls}, nil
		}
		return &types.GenerateResult{Text: "done", Content: []types.ContentPart{types.TextContent{Text: "done"}}, FinishReason: types.FinishReasonStop}, nil
	}}
}

// TS generateText (ToolLoopAgent.generate, WorkflowAgent generate mode): a
// `{type: 'user-approval', reason}` status puts the reason on the
// tool-approval-request, in the step content and in the assistant response
// message. Expected values from a TS 9f7a842ae1 run (generateText with
// toolApproval {a: {type: 'user-approval', reason: 'check'}}).
func TestR90genLegacyUserApprovalReason(t *testing.T) {
	reason := "check"
	a := NewToolLoopAgent(AgentConfig{
		Model: r90genTwoStepModel([]types.ToolCall{{ID: "c1", ToolName: "a", RawArguments: `{}`}, {ID: "c2", ToolName: "b", RawArguments: `{}`}}),
		Tools: []types.Tool{
			{Name: "a", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "r", nil
			}},
			{Name: "b", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return nil, errors.New("boom")
			}},
		},
		ToolApproval: map[string]types.ToolApprovalValue{"a": types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval, Reason: &reason}},
		StopWhen:     []ai.StopCondition{ai.IsStepCount(3)},
	})
	result, err := a.Execute(context.Background(), "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) != 1 {
		t.Fatalf("steps = %d, want 1 (the loop waits for the approval)", len(result.Steps))
	}
	step := result.Steps[0]
	var contentReason, messageReason string
	found := 0
	for _, part := range step.Content {
		if p, ok := part.(types.ToolApprovalRequestContent); ok {
			found++
			contentReason = p.Reason
		}
	}
	for _, m := range step.Response.Messages {
		for _, part := range m.Content {
			if p, ok := part.(types.ToolApprovalRequestContent); ok {
				found++
				messageReason = p.Reason
			}
		}
	}
	if found != 2 || contentReason != "check" || messageReason != "check" {
		t.Fatalf("approval request reasons: found %d, content %q, message %q; want 2, \"check\", \"check\"", found, contentReason, messageReason)
	}
}

// r90genStructuredTextModel returns two text parts and a provider-selected
// structured output text (TS structuredOutputText, OpenAI Responses final
// answer).
func r90genStructuredTextModel(texts []string, selected string) *functionalAgentLanguageModel {
	return &functionalAgentLanguageModel{doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		var content []types.ContentPart
		full := ""
		for _, text := range texts {
			content = append(content, types.TextContent{Text: text})
			full += text
		}
		return &types.GenerateResult{Text: full, Content: content, StructuredOutputText: &selected, FinishReason: types.FinishReasonStop}, nil
	}}
}

// TS generate-text.test.ts 9195-9330: a JSON output parses the last model
// response's structuredOutputText, the text keeps every part, an invalid
// selected text is rejected without falling back, and a text output keeps
// all text.
func TestR90genLegacyStructuredOutputText(t *testing.T) {
	valueSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
		"required":             []interface{}{"value"},
		"additionalProperties": false,
	})
	t.Run("object output uses the selected text", func(t *testing.T) {
		a := NewToolLoopAgent(AgentConfig{
			Model:  r90genStructuredTextModel([]string{`{"value":"draft"}`, `{"value":"final"}`}, `{"value":"final"}`),
			Output: ai.ObjectOutput[map[string]interface{}](ai.ObjectOutputOptions{Schema: valueSchema}),
		})
		result, err := a.Execute(context.Background(), "prompt")
		if err != nil {
			t.Fatal(err)
		}
		got, _ := result.Output.(map[string]interface{})
		if got["value"] != "final" {
			t.Fatalf("output = %#v, want {value: final}", result.Output)
		}
		if result.Text != `{"value":"draft"}{"value":"final"}` {
			t.Fatalf("text = %q", result.Text)
		}
	})
	for _, selected := range []string{"", `{"value":`} {
		t.Run("invalid selected text "+selected, func(t *testing.T) {
			a := NewToolLoopAgent(AgentConfig{
				Model:  r90genStructuredTextModel([]string{`{"value":"draft"}`}, selected),
				Output: ai.ObjectOutput[map[string]interface{}](ai.ObjectOutputOptions{Schema: valueSchema}),
			})
			_, err := a.Execute(context.Background(), "prompt")
			var noObject *ai.NoObjectGeneratedError
			if !errors.As(err, &noObject) || noObject.Text != selected {
				t.Fatalf("err = %v, want NoObjectGeneratedError with text %q", err, selected)
			}
		})
	}
	t.Run("text output keeps all text", func(t *testing.T) {
		a := NewToolLoopAgent(AgentConfig{
			Model:  r90genStructuredTextModel([]string{"Commentary. ", "Final answer."}, "Final answer."),
			Output: ai.TextOutput(),
		})
		result, err := a.Execute(context.Background(), "prompt")
		if err != nil {
			t.Fatal(err)
		}
		if result.Output != "Commentary. Final answer." {
			t.Fatalf("output = %#v", result.Output)
		}
	})
}

// TS WorkflowAgent's generate mode parses the last step's text, not the
// structured output text (workflow-agent.ts 1666-1681).
func TestR90genWorkflowRunParsesStepText(t *testing.T) {
	valueSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
		"required":   []interface{}{"value"},
	})
	a := NewToolLoopAgent(AgentConfig{
		Model:  r90genStructuredTextModel([]string{`{"value":"draft"}`}, `{"value":"final"}`),
		Output: ai.ObjectOutput[map[string]interface{}](ai.ObjectOutputOptions{Schema: valueSchema}),
	})
	result, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "prompt", Internal: &ai.InternalOptions{ContinueOnMissingProviderResult: true}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := result.Output.(map[string]interface{}); got["value"] != "draft" {
		t.Fatalf("output = %#v, want {value: draft}", result.Output)
	}
}

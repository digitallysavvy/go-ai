package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS generateText runs prepareTools (tool descriptions) and
// convertToLanguageModelPrompt before prepareStepCallSettings validates a
// prepareStep maxOutputTokens override, and the step start event comes after.
func TestGenerateTextPrepareStepMaxOutputTokensCheckedAfterPromptConversion(t *testing.T) {
	zero := 0
	described := false
	stepStarted := false
	tool := types.Tool{
		Name:       "lookup",
		Parameters: map[string]interface{}{"type": "object"},
		DescriptionFunc: func(context.Context, types.ToolDescriptionOptions) string {
			described = true
			return "look things up"
		},
	}
	prepare := func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
		return PrepareStepOptions{MaxOutputTokens: &zero}
	}
	opts := GenerateTextOptions{
		Model:       &testutil.MockLanguageModel{},
		Prompt:      "hi",
		Tools:       []types.Tool{tool},
		PrepareStep: prepare,
		OnStepStart: func(context.Context, OnStepStartEvent) { stepStarted = true },
	}
	if _, err := GenerateText(context.Background(), opts); !isMaxOutputTokensError(err) {
		t.Fatalf("err = %v, want the maxOutputTokens error", err)
	}
	if !described || stepStarted {
		t.Fatalf("described = %v, stepStarted = %v; want true, false", described, stepStarted)
	}

	// A prompt error found by the prompt conversion comes first.
	opts.Prompt = ""
	opts.Messages = []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "c1", ToolName: "lookup", Input: "{}"}}},
	}
	if _, err := GenerateText(context.Background(), opts); err == nil || isMaxOutputTokensError(err) {
		t.Fatalf("err = %v, want the missing tool result error", err)
	}
}

// TS streamText runs prepareTools (tool descriptions) before
// prepareStepCallSettings rejects a prepareStep maxOutputTokens override.
func TestStreamTextPrepareStepMaxOutputTokensCheckedAfterToolPreparation(t *testing.T) {
	zero := 0
	described := false
	tool := types.Tool{
		Name:       "lookup",
		Parameters: map[string]interface{}{"type": "object"},
		DescriptionFunc: func(context.Context, types.ToolDescriptionOptions) string {
			described = true
			return "look things up"
		},
	}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:  &testutil.MockLanguageModel{},
		Prompt: "hi",
		Tools:  []types.Tool{tool},
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{MaxOutputTokens: &zero}
		},
	})
	if err != nil {
		t.Fatalf("StreamText err = %v", err)
	}
	if _, err := res.ReadAll(); !isMaxOutputTokensError(err) {
		t.Fatalf("stream err = %v, want the maxOutputTokens error", err)
	}
	if !described {
		t.Fatal("tool description was not resolved before the settings check")
	}
}

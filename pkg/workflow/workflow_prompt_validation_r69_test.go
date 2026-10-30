package workflow

import (
	"context"
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r69UserMessages(text string) []types.Message {
	return []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: text}}}}
}

func r69WantInvalidPrompt(t *testing.T, name string, err error, want string) {
	t.Helper()
	var invalid *providererrors.InvalidPromptError
	if !errors.As(err, &invalid) {
		t.Fatalf("%s: error = %v (%T), want *InvalidPromptError", name, err, err)
	}
	if err.Error() != want {
		t.Fatalf("%s: error = %q, want %q", name, err.Error(), want)
	}
}

// TS WorkflowAgent standardizes its prompt after prepareCall: a call without
// a prompt or messages is an InvalidPromptError ("prompt or messages must be
// defined") unless prepareCall supplies the messages; both at once is the
// standardizePrompt error; unsupported instructions are its instructions
// error.
func TestR69WorkflowPromptValidationFollowsStandardizePrompt(t *testing.T) {
	ctx := context.Background()
	a, err := NewWorkflowAgent(WorkflowAgent{Model: &wfMockModel{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.GenerateWithOptions(ctx, WorkflowGenerateOptions{})
	r69WantInvalidPrompt(t, "generate without prompt", err, "Invalid prompt: prompt or messages must be defined")
	_, err = a.StreamWithOptions(ctx, WorkflowStreamOptions{})
	r69WantInvalidPrompt(t, "stream without prompt", err, "Invalid prompt: prompt or messages must be defined")
	_, err = a.GenerateWithOptions(ctx, WorkflowGenerateOptions{Messages: []types.Message{}})
	r69WantInvalidPrompt(t, "generate with empty messages", err, "Invalid prompt: messages must not be empty")

	_, err = a.GenerateWithOptions(ctx, WorkflowGenerateOptions{Prompt: "p", Messages: r69UserMessages("m")})
	r69WantInvalidPrompt(t, "generate with both", err, "Invalid prompt: prompt and messages cannot be defined at the same time")
	_, err = a.StreamWithOptions(ctx, WorkflowStreamOptions{Prompt: "p", Messages: r69UserMessages("m")})
	r69WantInvalidPrompt(t, "stream with both", err, "Invalid prompt: prompt and messages cannot be defined at the same time")

	_, err = a.GenerateWithOptions(ctx, WorkflowGenerateOptions{Prompt: "p", Instructions: 42})
	r69WantInvalidPrompt(t, "generate with bad instructions", err, "Invalid prompt: instructions must be a string, SystemModelMessage, or array of SystemModelMessage")
	_, err = NewWorkflowAgent(WorkflowAgent{Model: &wfMockModel{}, Instructions: 42})
	r69WantInvalidPrompt(t, "agent with bad instructions", err, "Invalid prompt: instructions must be a string, SystemModelMessage, or array of SystemModelMessage")

	withHook, err := NewWorkflowAgent(WorkflowAgent{
		Model: &wfMockModel{},
		PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
			o.Messages = r69UserMessages("from prepareCall")
			return o, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := withHook.GenerateWithOptions(ctx, WorkflowGenerateOptions{}); err != nil {
		t.Fatalf("generate with prepareCall messages: %v", err)
	}
	res, err := withHook.StreamWithOptions(ctx, WorkflowStreamOptions{})
	if err != nil {
		t.Fatalf("stream with prepareCall messages: %v", err)
	}
	_ = res.ConsumeStream()
}

// standardizePrompt checks a missing prompt before the instructions.
func TestR69WorkflowMissingPromptBeforeInstructions(t *testing.T) {
	a, err := NewWorkflowAgent(WorkflowAgent{Model: &wfMockModel{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Instructions: 42})
	r69WantInvalidPrompt(t, "generate", err, "Invalid prompt: prompt or messages must be defined")
	_, err = a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Instructions: 42})
	r69WantInvalidPrompt(t, "stream", err, "Invalid prompt: prompt or messages must be defined")
}

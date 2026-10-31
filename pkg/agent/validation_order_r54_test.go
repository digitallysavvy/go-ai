package agent

import (
	"context"
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// The legacy loop rejects an invalid ToolCallConcurrency when the run starts
// and an invalid MaxRetries before the model call (TS prepareToolCallConcurrency,
// then prepareRetries), so with both invalid the toolCallConcurrency error
// wins, and a negative MaxRetries alone never reaches the model.
func TestGenerateAgentValidatesToolCallConcurrencyThenMaxRetries(t *testing.T) {
	field := func(err error) string {
		var invalid *providererrors.InvalidArgumentError
		if !errors.As(err, &invalid) {
			return ""
		}
		return invalid.Field
	}
	maxRetries, concurrency := -1, 0
	model := &mockLanguageModel{}
	a := NewToolLoopAgent(AgentConfig{Model: model, MaxRetries: &maxRetries, ToolCallConcurrency: &concurrency})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); field(err) != "toolCallConcurrency" {
		t.Fatalf("both invalid: err = %v, want the toolCallConcurrency error", err)
	}
	a = NewToolLoopAgent(AgentConfig{Model: model, MaxRetries: &maxRetries})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); field(err) != "maxRetries" {
		t.Fatalf("negative MaxRetries: err = %v, want the maxRetries error", err)
	}
	if model.callCount != 0 {
		t.Fatalf("model called %d times", model.callCount)
	}
}

// TS generateText rejects maxOutputTokens < 1 (prepareLanguageModelCallOptions,
// after prepareRetries); the legacy loop does so before the model call.
func TestGenerateAgentRejectsMaxOutputTokensBelowOne(t *testing.T) {
	zero := 0
	model := &mockLanguageModel{}
	a := NewToolLoopAgent(AgentConfig{Model: model, MaxTokens: &zero})
	_, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"})
	var invalid *providererrors.InvalidArgumentError
	if !errors.As(err, &invalid) || invalid.Field != "maxOutputTokens" {
		t.Fatalf("err = %v, want the maxOutputTokens error", err)
	}
	if model.callCount != 0 {
		t.Fatalf("model called %d times", model.callCount)
	}
}

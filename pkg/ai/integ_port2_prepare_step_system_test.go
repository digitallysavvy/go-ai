package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// cr-port2-carry-to-gen item 2: TS checks system-role messages only in the
// call's prompt / messages (standardizePrompt); the messages a prepareStep
// returns go straight to convertToLanguageModelPrompt, so they may hold
// system messages without allowSystemInMessages
// (workflow-agent-tool-approval-config.test.ts "uses prepared pre-response
// history" returns [{role:'system'}, ...history]).

func integPort2SystemPrepareStep(_ context.Context, o PrepareStepOptions) PrepareStepOptions {
	o.Messages = append([]types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "prepared system rule"}}}}, o.Messages...)
	return o
}

func integPort2PromptHasSystem(opts *provider.GenerateOptions, text string) bool {
	if opts == nil {
		return false
	}
	if strings.Contains(opts.Prompt.System, text) {
		return true
	}
	for _, m := range opts.Prompt.Messages {
		if m.Role != types.RoleSystem {
			continue
		}
		for _, p := range m.Content {
			if tc, ok := p.(types.TextContent); ok && tc.Text == text {
				return true
			}
		}
	}
	return false
}

func TestIntegPort2PrepareStepMessagesMayHoldSystemMessages(t *testing.T) {
	t.Run("generate", func(t *testing.T) {
		model := &testutil.MockLanguageModel{}
		_, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hello", PrepareStep: integPort2SystemPrepareStep})
		if err != nil {
			t.Fatalf("GenerateText: %v", err)
		}
		if len(model.GenerateCalls) != 1 || !integPort2PromptHasSystem(model.GenerateCalls[0], "prepared system rule") {
			t.Fatalf("model did not get the prepared system message: %+v", model.GenerateCalls)
		}
	})
	t.Run("stream", func(t *testing.T) {
		model := &testutil.MockLanguageModel{}
		res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hello", PrepareStep: integPort2SystemPrepareStep})
		if err != nil {
			t.Fatalf("StreamText: %v", err)
		}
		if err := res.ConsumeStream(); err != nil {
			t.Fatalf("ConsumeStream: %v", err)
		}
		if len(model.StreamCalls) != 1 || !integPort2PromptHasSystem(model.StreamCalls[0], "prepared system rule") {
			t.Fatalf("model did not get the prepared system message: %+v", model.StreamCalls)
		}
	})
}

// The check on the caller's own messages stays: without the opt-in, a
// system message in Messages is rejected before any model call, also when a
// PrepareStep is configured.
func TestIntegPort2CallMessagesStillRejectSystemMessages(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "injected"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hello"}}},
	}
	passThrough := func(_ context.Context, o PrepareStepOptions) PrepareStepOptions { return o }
	t.Run("generate", func(t *testing.T) {
		model := &testutil.MockLanguageModel{}
		_, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Messages: msgs, PrepareStep: passThrough})
		if err == nil || !strings.Contains(err.Error(), "System messages are not allowed") {
			t.Fatalf("err = %v, want the system-message rejection", err)
		}
		if len(model.GenerateCalls) != 0 {
			t.Fatalf("model called %d times", len(model.GenerateCalls))
		}
	})
	t.Run("stream", func(t *testing.T) {
		model := &testutil.MockLanguageModel{}
		res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Messages: msgs, PrepareStep: passThrough})
		if err == nil && res != nil {
			err = res.ConsumeStream()
		}
		if err == nil || !strings.Contains(err.Error(), "System messages are not allowed") {
			t.Fatalf("err = %v, want the system-message rejection", err)
		}
		if len(model.StreamCalls) != 0 {
			t.Fatalf("model called %d times", len(model.StreamCalls))
		}
	})
}

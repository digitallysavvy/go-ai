package ai

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
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

// Twin of TS telemetry/tracing-channel.test.ts 583 "traces one step span when
// a steering interrupt redoes the generateText model call": one step start,
// two model-call starts for the same call (the redo carries the steering
// message), one model-call end and no error (the stream side is
// TestPort2streamInterruptTelemetryStartsASecondModelCallInTheStep).
func TestIntegPort2GenerateInterruptTelemetryStartsASecondModelCallInTheStep(t *testing.T) {
	steering := NewSteering()
	h := &portgenHangingModel{}
	model := h.model(func() { steering.Steer("Use Paris, Texas instead.", WithSteerInterrupt()) },
		func(*provider.GenerateOptions) *types.GenerateResult {
			return portgenText("It is sunny.", 3)
		})
	rec := &port2streamTelemetry{}
	enabled := true
	if _, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "What is the weather in Paris?", ExperimentalSteering: steering,
		ExperimentalTelemetry: &TelemetrySettings{IsEnabled: &enabled, Integrations: []telemetry.TelemetryIntegration{rec}},
	}); err != nil {
		t.Fatal(err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(h.calls) != 2 {
		t.Fatalf("model calls = %d, want 2", len(h.calls))
	}
	if rec.stepStarts != 1 {
		t.Fatalf("step starts = %d, want 1", rec.stepStarts)
	}
	if rec.callStarts != 2 || len(rec.callIDs) != 1 || !reflect.DeepEqual(rec.callPrompt, []int{1, 2}) {
		t.Fatalf("call starts = %d, call ids = %v, messages per call = %v", rec.callStarts, rec.callIDs, rec.callPrompt)
	}
	if rec.callEnds != 1 || rec.errors != 0 {
		t.Fatalf("call ends = %d, errors = %d, want 1 and 0", rec.callEnds, rec.errors)
	}
}

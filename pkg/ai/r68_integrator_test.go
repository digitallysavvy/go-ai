package ai

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r68IntegCtxKey string

// r68StepErrorCtxTelemetry marks the step and model-call contexts it returns
// and records which of them OnStepError gets.
type r68StepErrorCtxTelemetry struct {
	telemetry.NoopTelemetryIntegration
	mu              sync.Mutex
	calls           int
	inStep, inModel bool
}

func (r *r68StepErrorCtxTelemetry) OnStepStart(ctx context.Context, _ telemetry.TelemetryStepStartEvent) context.Context {
	return context.WithValue(ctx, r68IntegCtxKey("step"), true)
}

func (r *r68StepErrorCtxTelemetry) OnLanguageModelCallStart(ctx context.Context, _ telemetry.LanguageModelCallStartEvent) context.Context {
	return context.WithValue(ctx, r68IntegCtxKey("model"), true)
}

func (r *r68StepErrorCtxTelemetry) OnStepError(ctx context.Context, _ telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.inStep = ctx.Value(r68IntegCtxKey("step")) != nil
	r.inModel = ctx.Value(r68IntegCtxKey("model")) != nil
}

// GenerateText's telemetry OnStepError gets the model-call context while the
// model call runs (so the GenAI "chat" span closes too) and the step context
// for a failure after the call returned.
func TestR68IntegratorGenerateStepErrorTelemetryContext(t *testing.T) {
	cases := []struct {
		name      string
		model     *testutil.MockLanguageModel
		choice    types.ToolChoice
		wantModel bool
	}{
		{"model call fails", &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return nil, context.DeadlineExceeded
		}}, types.ToolChoice{}, true},
		{"tool choice violation after the call", &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "x", FinishReason: types.FinishReasonStop}, nil
		}}, types.RequiredToolChoice(), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &r68StepErrorCtxTelemetry{}
			_, err := GenerateText(context.Background(), GenerateTextOptions{
				Model: tc.model, Prompt: "q", ToolChoice: tc.choice,
				Tools:     []types.Tool{{Name: "a"}},
				Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}},
			})
			if err == nil {
				t.Fatal("expected an error")
			}
			rec.mu.Lock()
			defer rec.mu.Unlock()
			if rec.calls != 1 || !rec.inStep || rec.inModel != tc.wantModel {
				t.Fatalf("OnStepError calls=%d inStep=%v inModel=%v, want 1 call in the step ctx, model ctx=%v", rec.calls, rec.inStep, rec.inModel, tc.wantModel)
			}
		})
	}
}

// A tool-input-available chunk without an input leaves the part's input
// absent (TS input: chunk.input is undefined).
func TestR68IntegratorToolInputAvailableWithoutInput(t *testing.T) {
	state := newUIMessageCallbackState(nil, "m1")
	if err := applyUIStateChunk(state, UIMessageChunk{"type": "tool-input-available", "toolCallId": "c1", "toolName": "t"}, func(error) string { return "" }); err != nil {
		t.Fatal(err)
	}
	parts := uiParts(state.message)
	if len(parts) != 1 {
		t.Fatalf("%d parts, want 1", len(parts))
	}
	if v, ok := asUIPartChunk(parts[0])["input"]; ok {
		t.Fatalf("input = %#v present, want absent", v)
	}
}

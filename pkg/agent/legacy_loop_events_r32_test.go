package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// The AgentResult loop (Execute / GenerateAgent) dispatches the same
// language-model-call and step-end events as GenerateText, with the full
// step fields.
func TestLegacyLoopFiresLanguageModelCallAndFullStepEvents(t *testing.T) {
	model := &functionalAgentLanguageModel{
		doGenerate: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:             "done",
				FinishReason:     types.FinishReasonStop,
				ProviderMetadata: map[string]interface{}{"p": map[string]interface{}{"k": "v"}},
			}, nil
		},
	}
	var start *ai.LanguageModelCallStartEvent
	var end *ai.LanguageModelCallEndEvent
	var step *ai.OnStepFinishEvent
	temperature := 0.5
	a := NewToolLoopAgent(AgentConfig{
		Model:       model,
		System:      "be brief",
		Temperature: &temperature,
		OnLanguageModelCallStart: func(_ context.Context, e ai.LanguageModelCallStartEvent) {
			start = &e
		},
		OnLanguageModelCallEnd: func(_ context.Context, e ai.LanguageModelCallEndEvent) {
			end = &e
		},
		OnStepEndEvent: func(_ context.Context, e ai.OnStepFinishEvent) {
			step = &e
		},
	})
	if _, err := a.Execute(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if start == nil || start.Instructions != "be brief" || start.Temperature == nil || *start.Temperature != 0.5 || start.CallID == "" {
		t.Fatalf("start event = %+v", start)
	}
	if end == nil || end.FinishReason != types.FinishReasonStop || end.CallID != start.CallID || end.ResponseID == "" || len(end.Content) == 0 {
		t.Fatalf("end event = %+v", end)
	}
	if step == nil || step.CallID != start.CallID || step.Model.ModelID == "" || step.ProviderMetadata == nil || step.Response.ID == "" {
		t.Fatalf("step event = %+v", step)
	}
}

type startRecorder struct {
	telemetry.NoopTelemetryIntegration
	start *telemetry.TelemetryStartEvent
	end   *telemetry.LanguageModelCallEndEvent
}

func (r *startRecorder) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	r.start = &e
	return ctx
}

func (r *startRecorder) OnLanguageModelCallEnd(_ context.Context, e telemetry.LanguageModelCallEndEvent) {
	r.end = &e
}

// The AgentResult loop reports the request settings and headers on its root
// start event, and the response model and answer text on the model-call end
// event, like GenerateText.
func TestLegacyLoopTelemetryStartSettingsAndCallEnd(t *testing.T) {
	model := &functionalAgentLanguageModel{
		doGenerate: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop,
				ResponseMetadata: &types.ResponseMetadata{ID: "resp-1", ModelID: "served-model"}}, nil
		},
	}
	rec := &startRecorder{}
	temperature := 0.25
	maxTokens := 99
	a := NewToolLoopAgent(AgentConfig{
		Model:       model,
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
		Headers:     map[string]string{"x-a": "b"},
		Telemetry:   &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	if _, err := a.Execute(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if rec.start == nil || rec.start.Temperature == nil || *rec.start.Temperature != 0.25 ||
		rec.start.MaxOutputTokens == nil || *rec.start.MaxOutputTokens != 99 ||
		rec.start.Headers["x-a"] != "b" || rec.start.MaxRetries == nil || *rec.start.MaxRetries != 2 {
		t.Fatalf("start event = %+v", rec.start)
	}
	if rec.end == nil || rec.end.ModelID != "served-model" || rec.end.ResponseID != "resp-1" || rec.end.Content == nil {
		t.Fatalf("model call end = %+v", rec.end)
	}
}

package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

type r52EndRecorder struct {
	startEventRecorder
	ends []telemetry.TelemetryFinishEvent
}

func (r *r52EndRecorder) OnEnd(ctx context.Context, e telemetry.TelemetryFinishEvent) {
	r.ends = append(r.ends, e)
}

type r52NamedModel struct {
	mockLanguageModel
	id string
}

func (m *r52NamedModel) ModelID() string { return m.id }

// The legacy loop runs the first step's PrepareCall before it opens the root
// span (TS ToolLoopAgent applies prepareCall before generateText reports its
// start): the root start event and OnStartEvent carry the prepared model,
// instructions, messages, settings, headers and retry count; PrepareCall runs
// once for step 0; the end event reports the last step's model.
func TestR52LoopRootUsesPreparedCall(t *testing.T) {
	rec := &r52EndRecorder{}
	temp, prepTemp := 0.1, 0.9
	prepRetries := 5
	prepared := &r52NamedModel{id: "prepared-model"}
	calls := 0
	var started []ai.OnStartEvent
	a := NewToolLoopAgent(AgentConfig{
		Model:       &mockLanguageModel{},
		System:      "configured",
		Temperature: &temp,
		Telemetry:   &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}},
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			calls++
			c.Model = prepared
			c.System = "prepared"
			c.Temperature = &prepTemp
			c.MaxRetries = &prepRetries
			c.Headers = map[string]string{"x-prepared": "1"}
			c.Messages = append(append([]types.Message(nil), c.Messages...), types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "extra"}}})
			return c
		},
		OnStart: func(_ context.Context, e ai.OnStartEvent) { started = append(started, e) },
	})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("PrepareCall calls = %d, want 1 (one step)", calls)
	}
	if len(rec.starts) != 1 {
		t.Fatalf("start events = %d", len(rec.starts))
	}
	e := rec.starts[0]
	if e.ModelID != "prepared-model" || e.System != "prepared" || e.Temperature == nil || *e.Temperature != prepTemp ||
		e.MaxRetries == nil || *e.MaxRetries != prepRetries || e.Headers["x-prepared"] != "1" || len(e.Messages) != 2 {
		t.Fatalf("root start event = %+v", e)
	}
	if len(started) != 1 || started[0].ModelID != "prepared-model" || started[0].Instructions != "prepared" || len(started[0].Messages) != 2 {
		t.Fatalf("OnStartEvent = %+v", started)
	}
	if len(rec.ends) != 1 || rec.ends[0].ModelID != "prepared-model" {
		t.Fatalf("end events = %+v", rec.ends)
	}
}

// The legacy loop's end event carries the final step's fields like the TS
// generateText onEnd event: call id, step number, model, raw finish reason,
// response and contexts.
func TestR52LegacyLoopEndEventCarriesFinalStep(t *testing.T) {
	var ended []ai.OnFinishEvent
	a := NewToolLoopAgent(AgentConfig{
		Model:          &mockLanguageModel{responses: []types.GenerateResult{{Text: "done", FinishReason: types.FinishReasonStop, RawFinishReason: "end_turn"}}},
		RuntimeContext: map[string]interface{}{"tenant": "a"},
		PrepareCall: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			c.RuntimeContext = map[string]interface{}{"tenant": "prepared"}
			return c
		},
		OnEndEvent: func(_ context.Context, e ai.OnFinishEvent) { ended = append(ended, e) },
	})
	if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	if len(ended) != 1 {
		t.Fatalf("end events = %d", len(ended))
	}
	e := ended[0]
	rc, _ := e.RuntimeContext.(map[string]interface{})
	if e.CallID == "" || e.ModelID != "mock-model" || e.RawFinishReason != "end_turn" || e.FinalStep.Text != "done" || rc["tenant"] != "prepared" {
		t.Fatalf("end event = %+v", e)
	}
}

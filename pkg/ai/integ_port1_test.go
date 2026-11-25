package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Port wave 1 integration (TS 7760e33531, 71410a4755): the GenerateText end
// event carries stoppedBy, and the telemetry end event of GenerateText and
// StreamText carries the content of every step in order.
func TestIntegPort1EndEventsCarryStoppedByAndAllStepsContent(t *testing.T) {
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}}
	weather := types.Tool{Name: "weather", Parameters: schema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "sunny", nil
	}}
	call := types.ToolCall{ID: "call-1", ToolName: "weather", Arguments: map[string]interface{}{"city": "Paris"}, RawArguments: `{"city":"Paris"}`}
	for _, stream := range []bool{false, true} {
		step := 0
		model := &testutil.MockLanguageModel{
			ToolSupport: true,
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				step++
				if step == 1 {
					return &types.GenerateResult{
						Text:         "Let me check.",
						Content:      []types.ContentPart{types.TextContent{Text: "Let me check."}},
						ToolCalls:    []types.ToolCall{call},
						FinishReason: types.FinishReasonToolCalls,
					}, nil
				}
				return &types.GenerateResult{Text: "It is sunny.", Content: []types.ContentPart{types.TextContent{Text: "It is sunny."}}, FinishReason: types.FinishReasonStop}, nil
			},
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				step++
				if step == 1 {
					c := call
					return testutil.NewMockTextStream([]provider.StreamChunk{
						{Type: provider.ChunkTypeText, Text: "Let me check."},
						{Type: provider.ChunkTypeToolCall, ToolCall: &c},
						{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
					}), nil
				}
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "It is sunny."},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			},
		}
		rec := &r52EndModelRecorder{}
		settings := &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}}
		var finish *OnFinishEvent
		if stream {
			r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "weather?", Tools: []types.Tool{weather}, StopWhen: []StopCondition{IsStepCount(5)}, Telemetry: settings})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.ReadAll(); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := GenerateText(context.Background(), GenerateTextOptions{
				Model: model, Prompt: "weather?", Tools: []types.Tool{weather}, StopWhen: []StopCondition{IsStepCount(5)}, Telemetry: settings,
				OnFinishEvent: func(_ context.Context, e OnFinishEvent) { finish = &e },
			}); err != nil {
				t.Fatal(err)
			}
			if finish == nil || finish.StoppedBy != StoppedByModel {
				t.Fatalf("GenerateText end event StoppedBy = %+v", finish)
			}
		}
		rec.mu.Lock()
		if len(rec.ends) != 1 {
			t.Fatalf("stream=%v: %d end events", stream, len(rec.ends))
		}
		content := rec.ends[0].Content
		rec.mu.Unlock()
		var kinds []string
		for _, p := range content {
			switch v := p.(type) {
			case types.TextContent:
				kinds = append(kinds, "text:"+v.Text)
			case types.ToolCallContent:
				kinds = append(kinds, "tool-call")
			case types.ToolResultContent:
				kinds = append(kinds, "tool-result")
			}
		}
		want := []string{"text:Let me check.", "tool-call", "tool-result", "text:It is sunny."}
		if len(kinds) != len(want) {
			t.Fatalf("stream=%v: content kinds = %v, want %v", stream, kinds, want)
		}
		for i := range want {
			if kinds[i] != want[i] {
				t.Fatalf("stream=%v: content kinds = %v, want %v", stream, kinds, want)
			}
		}
	}
}

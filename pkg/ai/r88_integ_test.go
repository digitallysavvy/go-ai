package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// cr-r88-carry-to-integrator CR 1 (TS 8da48db187): the object callback events
// Go publishes on the ai:telemetry channel are filtered by the recording
// flags like every other channel message; the callbacks keep the full event.
func TestR88integObjectCallbackDiagnosticsFiltered(t *testing.T) {
	const secret = "r88integ-secret"
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			var mu sync.Mutex
			var all [][2]string // call id, event dump
			unsub := telemetry.SubscribeDiagnostic(func(_ context.Context, m telemetry.DiagnosticMessage) error {
				var id string
				switch e := m.Event.(type) {
				case ObjectOnStartEvent:
					id = e.CallID
				case ObjectOnStepStartEvent:
					id = e.CallID
				case ObjectOnStepFinishEvent:
					id = e.CallID
				case ObjectOnFinishEvent:
					id = e.CallID
				default:
					return nil
				}
				mu.Lock()
				all = append(all, [2]string{id, fmt.Sprintf("%#v", m.Event)})
				mu.Unlock()
				return nil
			})
			defer unsub()

			meta := map[string]interface{}{"p": map[string]interface{}{"k": secret + "-meta"}}
			model := &testutil.MockLanguageModel{
				ProviderName: "p", ModelName: "m", StructuredSupport: true,
				DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
					return &types.GenerateResult{Text: `{"name":"` + secret + `-object"}`, FinishReason: types.FinishReasonStop, ProviderMetadata: meta}, nil
				},
				DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
					return testutil.NewMockTextStream([]provider.StreamChunk{
						{Type: provider.ChunkTypeText, Text: `{"name":"` + secret + `-object"}`},
						{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, ProviderMetadata: json.RawMessage(`{"p":{"k":"` + secret + `-meta"}}`)},
					}), nil
				},
			}
			sch := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}}})
			settings := &telemetry.Options{IsEnabled: telemetry.Bool(true), RecordInputs: telemetry.Bool(false), RecordOutputs: telemetry.Bool(false), Integrations: []telemetry.TelemetryIntegration{}}
			var startPrompt, callID string
			var stepFinishText string
			onStart := func(_ context.Context, e ObjectOnStartEvent) { startPrompt, callID = e.Prompt, e.CallID }
			onStepFinish := func(_ context.Context, e ObjectOnStepFinishEvent) { stepFinishText = e.ObjectText }
			var err error
			if stream {
				var res *GenerateObjectResult
				res, err = StreamObject(context.Background(), StreamObjectOptions{
					Model: model, Prompt: secret + "-prompt", Schema: sch, SchemaName: secret + "-schema",
					ExperimentalTelemetry: settings, OnStart: onStart, OnStepFinish: onStepFinish,
				})
				_ = res
			} else {
				_, err = GenerateObject(context.Background(), GenerateObjectOptions{
					Model: model, Prompt: secret + "-prompt", Schema: sch, SchemaName: secret + "-schema",
					ExperimentalTelemetry: settings, OnStart: onStart, OnStepFinish: onStepFinish,
				})
			}
			if err != nil {
				t.Fatal(err)
			}
			if startPrompt != secret+"-prompt" || !strings.Contains(stepFinishText, secret+"-object") {
				t.Errorf("callbacks lost content: prompt %q, object text %q", startPrompt, stepFinishText)
			}
			mu.Lock()
			defer mu.Unlock()
			var objectEvents []string
			for _, e := range all {
				if e[0] == callID {
					objectEvents = append(objectEvents, e[1])
				}
			}
			if len(objectEvents) < 3 {
				t.Fatalf("object events on the channel = %d, want start, step start/finish and finish", len(objectEvents))
			}
			for _, e := range objectEvents {
				if strings.Contains(e, secret) {
					t.Errorf("channel event leaks hidden content: %s", e)
				}
			}
		})
	}
}

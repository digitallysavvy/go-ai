package ai

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/codes"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS stream-object.ts ends the telemetry of an error part or a final parse
// failure through the flush handler's onEnd, not onError: the root span gets
// the "error" finish-reason status for an error part, no status for a parse
// failure, and no recorded exception in either case.
func TestStreamObjectFailuresEndTelemetryLikeTS(t *testing.T) {
	for _, tc := range []struct {
		name       string
		chunks     []provider.StreamChunk
		wantStatus codes.Code
	}{
		{"error part", []provider.StreamChunk{
			{Type: provider.ChunkTypeError, Text: "boom"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}, codes.Error},
		{"parse failure", []provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: `{ broken`},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}, codes.Unset},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spanRecorder, cleanup := setupTelemetryTest(t)
			defer cleanup()
			model := &testutil.MockLanguageModel{
				StructuredSupport: true,
				DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
					return testutil.NewMockTextStream(tc.chunks), nil
				},
			}
			_, err := StreamObject(context.Background(), StreamObjectOptions{
				Model: model, Prompt: "p", Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
				ExperimentalTelemetry: &TelemetrySettings{IsEnabled: boolPtr(true)},
			})
			if err == nil {
				t.Fatal("want an error")
			}
			root := spanByOperationName(spanRecorder.Ended(), "ai.streamObject", "ai.streamObject")
			if root == nil {
				t.Fatal("no ended root span")
			}
			if root.Status().Code != tc.wantStatus {
				t.Errorf("root status = %v, want %v", root.Status(), tc.wantStatus)
			}
			for _, ev := range root.Events() {
				if ev.Name == "exception" {
					t.Errorf("root span recorded an exception: %v", ev.Attributes)
				}
			}
		})
	}
}

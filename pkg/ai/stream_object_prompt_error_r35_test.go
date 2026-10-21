package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// A prompt that fails standardization after the root span was opened must
// still close that span.
func TestStreamObject_PromptErrorClosesRootSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	settings, cleanup := legacyOnlySettings(rec)
	defer cleanup()
	model := &testutil.MockLanguageModel{
		ProviderName: "p", ModelName: "m",
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			t.Fatal("provider must not be called")
			return nil, nil
		},
	}
	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:     model,
		Schema:    schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
		Messages:  []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "sys"}}}},
		Telemetry: settings,
	})
	if err == nil {
		t.Fatal("want a prompt error")
	}
	assertNoOpenSpans(t, rec)
}

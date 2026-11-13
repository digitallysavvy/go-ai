package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// A PrepareStep that returns non-system instruction messages fails the stream;
// the root telemetry span must still be closed.
func TestStreamText_PrepareStepInstructionErrorClosesRootSpan(t *testing.T) {
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
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "hi", Telemetry: settings,
		PrepareStep: func(_ context.Context, p PrepareStepOptions) PrepareStepOptions {
			p.InstructionMessages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "x"}}}}
			return p
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, readErr := res.ReadAll(); readErr == nil {
		t.Fatal("want an instruction validation error")
	}
	assertNoOpenSpans(t, rec)
}

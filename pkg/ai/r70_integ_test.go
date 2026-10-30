package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

var r70IntegSchema = schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

// gen CR (r70): a model whose DoGenerate returns (nil, nil) fails
// GenerateObject (TS reads the response right after doGenerate) instead of
// panicking, with and without retries; GenerateObjectInto goes through the
// same path.
func TestR70Integ_GenerateObject_NilModelResultIsAnError(t *testing.T) {
	for _, retries := range []int{0, 2} {
		model := &testutil.MockLanguageModel{
			ProviderName:      "p",
			ModelName:         "m",
			StructuredSupport: true,
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return nil, nil
			},
		}
		n := retries
		_, err := GenerateObject(context.Background(), GenerateObjectOptions{
			Model: model, Prompt: "p", MaxRetries: &n,
			Schema: r70IntegSchema,
		})
		if err == nil || !strings.Contains(err.Error(), "language model p/m returned no result") {
			t.Fatalf("retries=%d: err = %v, want the no-result error", retries, err)
		}
	}
}

// StreamObject already reported a nil stream (v0.5.2 "stream is nil"); the
// text is kept.
func TestR70Integ_StreamObject_NilModelStreamIsAnError(t *testing.T) {
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, nil
		},
	}
	zero := 0
	_, err := StreamObject(context.Background(), StreamObjectOptions{Model: model, Prompt: "p", Schema: r70IntegSchema, MaxRetries: &zero})
	if err == nil || !strings.Contains(err.Error(), "stream is nil") {
		t.Fatalf("err = %v, want stream is nil", err)
	}
}

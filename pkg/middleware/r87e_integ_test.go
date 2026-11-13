package middleware

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r87eintegHeaderStream struct {
	*testutil.MockTextStream
}

func (r87eintegHeaderStream) ResponseHeaders() map[string]string {
	return map[string]string{"X-A": "1"}
}

// TS extract-reasoning / extract-json return {...rest, stream} and
// simulate-streaming returns response: result.response from doStream, so
// the response headers survive the wrapper (provider.StreamResponseHeaders,
// round 87e).
func TestR87eintegMiddlewareStreamsForwardResponseHeaders(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return r87eintegHeaderStream{testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			})}, nil
		},
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop, ResponseHeaders: map[string]string{"X-A": "1"}}, nil
		},
	}
	for name, mw := range map[string]*LanguageModelMiddleware{
		"extractReasoning":  ExtractReasoningMiddleware(&ExtractReasoningOptions{TagName: "think"}),
		"extractJSON":       ExtractJSONMiddleware(nil),
		"simulateStreaming": SimulateStreamingMiddleware(),
	} {
		wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{mw}, nil, nil)
		st, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		rh, ok := st.(provider.StreamResponseHeaders)
		if !ok || rh.ResponseHeaders()["X-A"] != "1" {
			t.Errorf("%s: response headers not forwarded (%T)", name, st)
		}
		_ = st.Close()
	}
}

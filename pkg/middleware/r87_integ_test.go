package middleware

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

const r87integURL = "https://model.example.test:8443/private/v1/chat/completions"

// r87integURLStream reports a request body and URL (provider.StreamRequestBody
// and provider.StreamRequestURL).
type r87integURLStream struct {
	*testutil.MockTextStream
	body interface{}
}

func (s *r87integURLStream) RequestBody() interface{} { return s.body }
func (s *r87integURLStream) RequestURL() string       { return r87integURL }

// TS extract-reasoning-middleware.ts / extract-json-middleware.ts return
// {...rest, stream} from doStream, and simulate-streaming-middleware.ts
// returns request: result.request from doGenerate, so the request body and
// URL (934100cad4) survive the wrapper. Go's wrapper streams implemented
// neither capability, so StreamText behind them reported no request.
func TestR87integMiddlewareStreamsForwardRequest(t *testing.T) {
	body := map[string]interface{}{"model": "m"}
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return &r87integURLStream{MockTextStream: testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), body: body}, nil
		},
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop, RawRequest: body, RequestURL: r87integURL}, nil
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
		rb, ok := st.(provider.StreamRequestBody)
		if !ok || !reflect.DeepEqual(rb.RequestBody(), body) {
			t.Errorf("%s: stream request body not forwarded (%T)", name, st)
		}
		ru, ok := st.(provider.StreamRequestURL)
		if !ok || ru.RequestURL() != r87integURL {
			t.Errorf("%s: stream request URL not forwarded (%T)", name, st)
		}
		_ = st.Close()
	}
}

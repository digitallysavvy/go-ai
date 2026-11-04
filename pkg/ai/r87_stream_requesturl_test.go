package ai

import (
	"context"
	"encoding/json"
	"regexp"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r87streamURLStream reports the URL it was opened with (the optional
// provider.StreamRequestURL capability, TS doStream() {request: {url}}).
type r87streamURLStream struct {
	provider.TextStream
	url string
}

func (s r87streamURLStream) RequestURL() string { return s.url }

// TS stream-text.test.ts "sanitizes streamed provider request URLs in results
// and step/end events" (934100cad4): the step result, finalStep, onStepEnd,
// onEnd and the model call end event carry only the origin, and no callback
// payload carries the credentials, path, query or fragment. The start-step
// part carries the same sanitized request (TS getStepRequest spreads it).
func TestR87streamRequestURLSanitized(t *testing.T) {
	const raw = "https://user:password@model.example.test:8443/private?key=secret#fragment"
	const origin = "https://model.example.test:8443"
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return r87streamURLStream{TextStream: testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "text"},
			{Type: provider.ChunkTypeText, ID: "text", Text: "hello"},
			{Type: provider.ChunkTypeTextEnd, ID: "text"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r72streamTestUsage},
		}), url: raw}, nil
	}}
	var mu sync.Mutex
	var payloads []interface{}
	var stepEnd OnStepFinishEvent
	var end OnFinishEvent
	var callEnd LanguageModelCallEndEvent
	record := func(v interface{}) {
		mu.Lock()
		payloads = append(payloads, v)
		mu.Unlock()
	}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hello",
		OnStepFinishEvent:      func(_ context.Context, e OnStepFinishEvent) { stepEnd = e; record(e) },
		OnEndEvent:             func(_ context.Context, e OnFinishEvent) { end = e; record(e) },
		OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) { callEnd = e; record(e) },
	})
	if err != nil {
		t.Fatal(err)
	}
	var startStep *types.StepRequest
	for c := range r.Chunks() {
		if c.Type == provider.ChunkTypeStartStep && c.Request != nil {
			req := *c.Request
			startStep = &req
		}
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := r.FinalStep().Request.URL; got != origin {
		t.Errorf("finalStep.request.url = %q, want %q", got, origin)
	}
	if steps := r.Steps(); len(steps) != 1 || steps[0].Request.URL != origin {
		t.Errorf("steps[0].request.url = %+v, want %q", steps, origin)
	}
	if startStep == nil || startStep.URL != origin {
		t.Errorf("start-step request = %+v, want url %q", startStep, origin)
	}
	if stepEnd.Request.URL != origin || end.Request.URL != origin {
		t.Errorf("onStepEnd / onEnd request.url = %q / %q, want %q", stepEnd.Request.URL, end.Request.URL, origin)
	}
	if callEnd.Request == nil || callEnd.Request.URL != origin {
		t.Errorf("model call end request = %+v, want url %q", callEnd.Request, origin)
	}
	b, err := json.Marshal(payloads)
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`password|private|secret|fragment`).Match(b) {
		t.Errorf("callback payloads leak URL parts: %s", b)
	}
}

// TS getRequestOrigin drops URLs that are not http(s) or do not parse: the
// step request has no url and the call end event no request.
func TestR87streamRequestURLNotHTTP(t *testing.T) {
	for _, raw := range []string{"", "not a URL", "/relative", "file:///etc/passwd", "data:text/plain,secret"} {
		model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return r87streamURLStream{TextStream: testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, ID: "1", Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r72streamTestUsage},
			}), url: raw}, nil
		}}
		var callEnd LanguageModelCallEndEvent
		r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "x",
			OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) { callEnd = e },
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.ReadAll(); err != nil {
			t.Fatal(err)
		}
		if got := r.FinalStep().Request.URL; got != "" {
			t.Errorf("%q: step request url = %q, want empty", raw, got)
		}
		if callEnd.Request != nil {
			t.Errorf("%q: call end request = %+v, want nil", raw, callEnd.Request)
		}
	}
}

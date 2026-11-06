package ai

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// requestBodyTextStream wraps a provider.TextStream and additionally
// implements provider.StreamRequestBody, mirroring a provider that surfaces
// the raw serialized request body it sent to open the stream.
type requestBodyTextStream struct {
	provider.TextStream
	body interface{}
}

func (s *requestBodyTextStream) RequestBody() interface{} { return s.body }

// TestStreamText_PopulatesStepRequestBodyFromStreamRequestBody verifies that
// when a provider's TextStream implements the optional
// provider.StreamRequestBody capability, StreamText copies it into the
// step's types.StepRequest.Body (hand-off: "stream request body field"),
// mirroring TS doStream() resolving {request: {body}} alongside {stream},
// when Include.RequestBody is set.
func TestStreamText_PopulatesStepRequestBodyFromStreamRequestBody(t *testing.T) {
	t.Parallel()

	wantBody := map[string]interface{}{"model": "test-model", "stream": true}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			base := testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: "stop"},
			})
			return &requestBodyTextStream{TextStream: base, body: wantBody}, nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:   model,
		Prompt:  "hi",
		Include: &IncludeOptions{RequestBody: true},
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}

	// Request()/Response() are only guaranteed to reflect the final step
	// once the background processStream goroutine has fully finished (its
	// legacy OnEnd/OnFinish callback fires slightly earlier, before these
	// accessors are populated) — ReadAll waits for that.
	readAllDone := make(chan struct{})
	go func() {
		defer close(readAllDone)
		_, _ = result.ReadAll()
	}()
	select {
	case <-readAllDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the stream to finish")
	}

	gotBody := result.Request().Body
	gotMap, ok := gotBody.(map[string]interface{})
	if !ok {
		t.Fatalf("Request().Body = %#v (%T), want the map passed via RequestBody()", gotBody, gotBody)
	}
	if gotMap["model"] != "test-model" || gotMap["stream"] != true {
		t.Fatalf("Request().Body = %#v, want %#v", gotMap, wantBody)
	}
}

// TS getStepRequest: include.requestBody defaults to false, so the step
// request, the start-step part and Request() carry no body unless asked.
func TestStreamText_RequestBodyOmittedByDefault(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			base := testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: "stop"},
			})
			return &requestBodyTextStream{TextStream: base, body: map[string]interface{}{"model": "m"}}, nil
		},
	}
	result, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi"})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	var startStepBody interface{} = "unset"
	for chunk := range result.Chunks() {
		if chunk.Type == provider.ChunkTypeStartStep && chunk.Request != nil {
			startStepBody = chunk.Request.Body
		}
	}
	if startStepBody != nil {
		t.Errorf("start-step request body = %#v, want nil", startStepBody)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if body := result.Steps()[0].Request.Body; body != nil {
		t.Errorf("step request body = %#v, want nil", body)
	}
	if body := result.Request().Body; body != nil {
		t.Errorf("Request().Body = %#v, want nil", body)
	}
}

// The GenerateText step-end event is the step result in TS, so its request
// and response bodies follow Include (both default to false).
func TestGenerateText_StepEndEventBodiesFollowInclude(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text: "hi", FinishReason: types.FinishReasonStop,
				RawRequest: map[string]interface{}{"req": 1}, RawResponse: map[string]interface{}{"res": 1},
			}, nil
		},
	}
	for _, include := range []*IncludeOptions{nil, {RequestBody: true, ResponseBody: true}} {
		var event OnStepFinishEvent
		_, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: model, Prompt: "hi", Include: include,
			OnStepEndEvent: func(_ context.Context, e OnStepFinishEvent) { event = e },
		})
		if err != nil {
			t.Fatalf("GenerateText() error = %v", err)
		}
		want := include != nil
		if (event.Request.Body != nil) != want || (event.Response.Body != nil) != want {
			t.Errorf("include=%v: step-end request body %#v, response body %#v", include, event.Request.Body, event.Response.Body)
		}
	}
}

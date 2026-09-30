package ai

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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
// mirroring TS doStream() resolving {request: {body}} alongside {stream}.
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
		Model:  model,
		Prompt: "hi",
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

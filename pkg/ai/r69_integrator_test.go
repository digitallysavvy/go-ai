package ai

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestR69IntegratorRetriedErrorPartDoesNotHideLaterPart: TS adds a model
// error part to errorsHandledForStreamRetry only when it is forwarded (no
// retry). A retried part reached onChunk once; a retry whose model call then
// fails with the same error value streams a new error part, which reaches
// onChunk and onError again.
func TestR69IntegratorRetriedErrorPartDoesNotHideLaterPart(t *testing.T) {
	e := errors.New("same")
	var mu sync.Mutex
	calls := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeError, Err: e, Text: e.Error()}}), nil
		}
		return nil, e
	}}
	one := 1
	var log r69EventLog
	result, err := StreamText(context.Background(), log.options(StreamTextOptions{Model: model, StreamRetries: &one}))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = result.ReadAll()
	if got, want := log.String(), "chunk:start chunk:error:same onError:same chunk:error:same onError:same"; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
}

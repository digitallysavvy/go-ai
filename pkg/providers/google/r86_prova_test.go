package google

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS resolves the provider headers once per streaming call: the APIKeyFunc
// runs once when a Live transcription stream opens (Go called it twice).
func TestR86provaLiveTranscriptionKeyFuncCalledOnce(t *testing.T) {
	server := newLiveTranscriptionTestServer(t)
	defer server.close()
	var calls int32
	p := New(Config{BaseURL: server.ts.URL, APIKeyFunc: func(context.Context) (string, error) {
		atomic.AddInt32(&calls, 1)
		return "func-key", nil
	}})
	m := NewTranscriptionModel(p, ModelGemini35TranscribeLive)
	result, err := m.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("APIKeyFunc calls = %d, want 1", got)
	}
}

type r86provaCtxKey struct{}

// The video download key (TS resolves config.headers for the download URL)
// comes from an APIKeyFunc called with the status call's ctx.
func TestR86provaVideoDownloadKeyFuncGetsCallerCtx(t *testing.T) {
	var withoutCtx int32
	p := New(Config{APIKeyFunc: func(ctx context.Context) (string, error) {
		if ctx.Value(r86provaCtxKey{}) == nil {
			atomic.AddInt32(&withoutCtx, 1)
		}
		return "func-key", nil
	}})
	m := NewVideoModel(p, "veo-3.0-generate-001")
	var op googleVideoOperationWire
	if err := json.Unmarshal([]byte(`{"name":"op","done":true,"response":{"generateVideoResponse":{"generatedSamples":[{"video":{"uri":"https://generativelanguage.googleapis.com/v1beta/files/x:download?alt=media"}}]}}}`), &op); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), r86provaCtxKey{}, true)
	res, err := m.buildCompletedResult(ctx, &op, provider.VideoModelV3ResponseInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&withoutCtx) != 0 || len(res.Videos) != 1 || !strings.Contains(res.Videos[0].URL, "key=func-key") {
		t.Fatalf("calls without the caller ctx = %d, videos = %+v", withoutCtx, res.Videos)
	}
}

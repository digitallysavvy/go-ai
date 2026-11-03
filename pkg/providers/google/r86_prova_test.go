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

// Batch uploads/downloads, file uploads and realtime tokens resolve the
// APIKeyFunc once, with the caller's ctx (Go also called it through a
// ctx-less header snapshot).
func TestR86provaAuthHeadersKeyFuncOnceWithCtx(t *testing.T) {
	var calls, withoutCtx int32
	p := New(Config{APIKeyFunc: func(ctx context.Context) (string, error) {
		atomic.AddInt32(&calls, 1)
		if ctx.Value(r86provaCtxKey{}) == nil {
			atomic.AddInt32(&withoutCtx, 1)
		}
		return "func-key", nil
	}})
	ctx := context.WithValue(context.Background(), r86provaCtxKey{}, true)
	h, err := p.authHeaders(ctx)
	if err != nil || h["x-goog-api-key"] != "func-key" {
		t.Fatalf("headers = %v, err = %v", h, err)
	}
	key, err := (&GoogleRealtimeModel{provider: p}).googleRealtimeAPIKey(ctx)
	if err != nil || key != "func-key" {
		t.Fatalf("realtime key = %q, err = %v", key, err)
	}
	if calls != 2 || withoutCtx != 0 {
		t.Fatalf("calls = %d, without the caller ctx = %d", calls, withoutCtx)
	}
}

// cr-r86-provb-to-prova item 3: a []map[string]interface{} responseFormat
// is read like its JSON form.
func TestR86provaInteractionsTypedResponseFormat(t *testing.T) {
	entry := map[string]interface{}{"type": "text"}
	typed, err := parseInteractionsProviderOptions(map[string]interface{}{"google": map[string]interface{}{"responseFormat": []map[string]interface{}{entry}}})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := parseInteractionsProviderOptions(map[string]interface{}{"google": map[string]interface{}{"responseFormat": []interface{}{entry}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(typed.ResponseFormat) != 1 || len(plain.ResponseFormat) != 1 {
		t.Fatalf("typed %v, plain %v", typed.ResponseFormat, plain.ResponseFormat)
	}
}

// Class sweep of cr-r86-provb-to-prova: an environment map holding typed Go
// slices (sources, network.allowlist) reads like its JSON form; it failed
// validation ("expected array") or dropped the sources.
func TestR86provaInteractionsTypedEnvironment(t *testing.T) {
	env := map[string]interface{}{
		"type":    "remote",
		"sources": []map[string]interface{}{{"type": "inline", "content": "x", "target": "/a"}},
		"network": map[string]interface{}{"allowlist": []map[string]interface{}{{"domain": "example.com"}}},
	}
	for name, raw := range map[string]interface{}{
		"map":    map[string]interface{}{"environment": env},
		"struct": GoogleInteractionsProviderOptions{Environment: env},
	} {
		opts, err := parseInteractionsProviderOptions(map[string]interface{}{"google": raw})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		wire, _ := json.Marshal(interactionsEnvironment(opts.Environment))
		want := `{"network":{"allowlist":[{"domain":"example.com"}]},"sources":[{"content":"x","target":"/a","type":"inline"}],"type":"remote"}`
		if string(wire) != want {
			t.Fatalf("%s: environment = %s", name, wire)
		}
	}
}

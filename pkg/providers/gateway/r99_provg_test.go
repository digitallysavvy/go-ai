package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS createAuthHeaders spreads options.headers after the protocol version and
// team headers, and withUserAgentSuffix lower-cases the names in entry order,
// so a custom header in another case replaces the SDK's. Go copied the
// custom headers beside the SDK keys and the sorted lower-casing kept the
// SDK's lower-case value.
func TestR99provgCustomHeaderInOtherCaseWins(t *testing.T) {
	var mu sync.Mutex
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[],"finishReason":"stop","usage":{}}`))
	}))
	defer server.Close()

	p, err := New(Config{
		APIKey: "test-key", BaseURL: server.URL, TeamIDOrSlug: "team_config",
		Headers: map[string]string{
			"AI-Gateway-Protocol-Version": "9.9.9",
			"X-Vercel-AI-Gateway-Team":    "team_custom",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := p.LanguageModel("openai/gpt-4o")
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if v := got.Values("Ai-Gateway-Protocol-Version"); len(v) != 1 || v[0] != "9.9.9" {
		t.Fatalf("ai-gateway-protocol-version = %q, want the custom header", v)
	}
	if v := got.Values("X-Vercel-Ai-Gateway-Team"); len(v) != 1 || v[0] != "team_custom" {
		t.Fatalf("x-vercel-ai-gateway-team = %q, want the custom header", v)
	}
}

// TS postJsonToApi sends {'Content-Type': 'application/json', ...headers}:
// a custom Content-Type replaces the JSON one on the client secret mint.
func TestR99provgMintClientSecretCustomContentTypeWins(t *testing.T) {
	var contentType []string
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Values("Content-Type")
		_, _ = w.Write([]byte(`{"token":"vcst_test"}`))
	}))
	defer closeServer()
	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai", Headers: map[string]string{"content-type": "application/vnd.custom+json"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.MintRealtimeClientSecret(context.Background(), MintRealtimeClientSecretParams{ModelID: "openai/gpt-realtime"}); err != nil {
		t.Fatal(err)
	}
	if len(contentType) != 1 || contentType[0] != "application/vnd.custom+json" {
		t.Fatalf("content-type = %q, want the custom header", contentType)
	}
}

// r99provgHeaderServer records the request headers of every call.
func r99provgHeaderServer(t *testing.T, body string, contentType string) (*httptest.Server, func() http.Header) {
	t.Helper()
	var mu sync.Mutex
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	return srv, func() http.Header {
		mu.Lock()
		defer mu.Unlock()
		return got.Clone()
	}
}

// TS gateway doStream sends combineHeaders(config, options.headers, model,
// o11y) with no Accept; Go adds its streaming default (known difference
// "Accept header"), which a call Accept must replace. Go set it after the
// call headers and overwrote the caller's.
func TestR99provgLanguageModelStreamCallAcceptWins(t *testing.T) {
	srv, headers := r99provgHeaderServer(t, "data: [DONE]\n\n", "text/event-stream")
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := p.LanguageModel("openai/gpt-4o")
	for i := 0; i < 8; i++ {
		stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
			Prompt:  types.Prompt{Text: "hi"},
			Headers: map[string]string{"Accept": "application/x-ndjson"},
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = stream.Close()
		if got := headers().Values("Accept"); len(got) != 1 || got[0] != "application/x-ndjson" {
			t.Fatalf("run %d: accept = %q, want the call header", i, got)
		}
	}
}

// The o11y headers come last in TS combineHeaders and replace a call header
// of the same name in any case; the video SSE accept comes after them.
func TestR99provgO11yAndVideoAcceptReplaceCallHeadersInAnyCase(t *testing.T) {
	t.Setenv("VERCEL_DEPLOYMENT_ID", "dpl_env")
	srv, headers := r99provgHeaderServer(t, `data: {"type":"result","videos":[{"type":"url","url":"https://x/v.mp4","mediaType":"video/mp4"}]}`+"\n\n", "text/event-stream")
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := NewVideoModel(p, "google/veo-3").DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
			Prompt: "p", N: 1,
			Headers: map[string]string{"AI-O11y-Deployment-Id": "dpl_call", "accept": "application/json"},
		}); err != nil {
			t.Fatal(err)
		}
		h := headers()
		if got := h.Values("Ai-O11y-Deployment-Id"); len(got) != 1 || got[0] != "dpl_env" {
			t.Fatalf("run %d: ai-o11y-deployment-id = %q, want the o11y value", i, got)
		}
		if got := h.Values("Accept"); len(got) != 1 || got[0] != "text/event-stream" {
			t.Fatalf("run %d: accept = %q, want text/event-stream", i, got)
		}
	}
}

// TS gateway-batch.ts combines the idempotency-key header last.
func TestR99provgBatchIdempotencyKeyReplacesCallHeader(t *testing.T) {
	srv, headers := r99provgHeaderServer(t, `{"batchId":"job_123","status":"pending"}`, "application/json")
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := p.ExperimentalBatch().DoStartBatch(context.Background(), provider.BatchV4StartOptions{
			Requests: []provider.BatchV4Request{testBatchTextRequest("req-1", "test-model-1")},
			Headers:  map[string]string{"Idempotency-Key": "from-call"},
			ProviderOptions: map[string]interface{}{
				"gateway": map[string]interface{}{"idempotencyKey": "idem-abc"},
			},
		}); err != nil {
			t.Fatal(err)
		}
		if got := headers().Values("Idempotency-Key"); len(got) != 1 || got[0] != "idem-abc" {
			t.Fatalf("run %d: idempotency-key = %q, want the option", i, got)
		}
	}
}

// The streaming transcription handshake carries the o11y headers last too.
func TestR99provgTranscriptionStreamO11yReplacesCallHeader(t *testing.T) {
	t.Setenv("VERCEL_DEPLOYMENT_ID", "dpl_env")
	for i := 0; i < 8; i++ {
		server := newGatewayTranscriptionTestServer(t)
		model := newTestGatewayTranscriptionModel(t, server.ts.URL)
		result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
			Audio:            newChanTestAudioStream(),
			InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
			Headers:          map[string]string{"AI-O11y-Deployment-Id": "dpl_call"},
		})
		if err != nil {
			t.Fatal(err)
		}
		server.waitForFrame(t, transcriptionStreamStartFrameType, time.Second)
		server.mu.Lock()
		got := server.requestHeaders.Values("Ai-O11y-Deployment-Id")
		server.mu.Unlock()
		_ = result.Stream.Close()
		server.close()
		if len(got) != 1 || got[0] != "dpl_env" {
			t.Fatalf("run %d: ai-o11y-deployment-id = %q, want the o11y value", i, got)
		}
	}
}

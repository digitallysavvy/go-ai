package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Credential transports (S2, round 88 sweep, related to TS 977231e913): the
// Google token and custom headers go only to the Vertex Anthropic origin; a
// cross-origin redirect hop receives neither, and a foreign request never
// asks for a token.
func TestR88provaVertexAnthropicCredentialsSameOriginOnly(t *testing.T) {
	base, foreign, api := r88provaRedirectServers(t)
	var tokens int32
	p := New(Options{
		BaseURL:   base,
		AuthToken: func(context.Context) (string, error) { atomic.AddInt32(&tokens, 1); return "tok", nil },
		Headers:   map[string]string{"X-Custom-Header": "secret"},
	})
	m, err := p.LanguageModel("claude-sonnet-4-5@20250929")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if len(api()) != 1 || api()[0].Get("Authorization") != "Bearer tok" || api()[0].Get("X-Custom-Header") != "secret" {
		t.Fatalf("API request headers = %v", api())
	}
	if len(foreign()) != 1 {
		t.Fatalf("foreign requests = %d", len(foreign()))
	}
	for _, h := range r88provaCredentialHeaders {
		if v := foreign()[0].Get(h); v != "" {
			t.Fatalf("foreign origin received %s: %q", h, v)
		}
	}
	if tokens != 1 {
		t.Fatalf("token func calls = %d", tokens)
	}

	// a foreign URL through the transport directly
	tr := p.httpClient(base).Transport
	req, _ := http.NewRequest(http.MethodGet, "https://example.invalid/image.png", nil)
	rec := &r88provaRecordingRT{}
	tr.(*authTransport).base = rec
	if _, err := tr.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if rec.header.Get("Authorization") != "" || rec.header.Get("X-Custom-Header") != "" || tokens != 1 {
		t.Fatalf("foreign request headers = %v, token calls = %d", rec.header, tokens)
	}
}

type r88provaRecordingRT struct{ header http.Header }

func (r *r88provaRecordingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	r.header = req.Header.Clone()
	return &http.Response{StatusCode: 200, Body: http.NoBody, Request: req}, nil
}

// TS 6b101ac076: Vertex Anthropic rejects message threads before a request.
func TestR88provaVertexAnthropicRejectsThreads(t *testing.T) {
	p := New(Options{BaseURL: "http://127.0.0.1:1", AuthToken: func(context.Context) (string, error) { return "tok", nil }})
	m, _ := p.LanguageModel("claude-sonnet-4-6")
	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"thread": map[string]interface{}{"type": "create"}}}})
	if err == nil || err.Error() != "googleVertex.anthropic.messages does not support Anthropic message threads." {
		t.Fatalf("err = %v", err)
	}
}

// TS 4b8f00efff: the native-output opt-in is gated by Vertex model support.
func TestR88provaVertexNativeOutputModels(t *testing.T) {
	for id, want := range map[string]bool{
		"claude-opus-4-5@20251101": true, "claude-sonnet-4-5@20250929": true, "claude-haiku-4-5@20251001": true,
		"claude-sonnet-4-6": true, "claude-opus-4-8": true, "claude-opus-5": true, "claude-opus-5-5": true,
		"claude-fable-5-1": true, "claude-sonnet-6": true, "claude-sonnet-5": true, "claude-sonnet-5-5": true,
		"claude-haiku-5-5": true, "claude-fable-5": true,
		"claude-sonnet-4-5-20250929": false, "claude-3-5-sonnet-v2@20241022": false, "claude-opus-4-1@20250805": false,
		"claude-opus-4@20250514": false, "claude-sonnet-4@20250514": false, "claude-3-7-sonnet@20250219": false,
		"custom-deployment": false, "claude-opus-4-4": false, "claude-opus-3-9": false,
	} {
		if got := vertexAnthropicSupportsNativeStructuredOutput(id); got != want {
			t.Errorf("%s: %v", id, got)
		}
	}
}

// TS 4b8f00efff google-vertex-anthropic-structured-output.test.ts: with the
// opt-in, auto mode sends output_config.format without the strict-tool beta;
// without it the JSON tool is used.
func TestR88provaVertexNativeOutputOptIn(t *testing.T) {
	for _, optIn := range []bool{false, true} {
		var body map[string]interface{}
		var beta string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			beta = r.Header.Get("anthropic-beta")
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"m","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"{}"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
		}))
		p := New(Options{BaseURL: srv.URL, AuthToken: func(context.Context) (string, error) { return "tok", nil }, SupportsNativeStructuredOutput: optIn})
		m, _ := p.LanguageModel("claude-sonnet-4-6")
		_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"},
			Tools:          []types.Tool{{Name: "lookup", Parameters: map[string]interface{}{"type": "object"}}},
			ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}}})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		_, native := body["output_config"]
		if native != optIn || beta != "" {
			t.Fatalf("optIn=%v: output_config %v beta %q tools %v", optIn, body["output_config"], beta, body["tools"])
		}
	}
}

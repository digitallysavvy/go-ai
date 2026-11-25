package googlevertex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	vertexinternal "github.com/digitallysavvy/go-ai/pkg/providers/googlevertex/internal"
)

// r88provgHeaderRecorder records the credential headers each request
// carried.
type r88provgHeaderRecorder struct {
	mu   sync.Mutex
	seen []http.Header
}

func (r *r88provgHeaderRecorder) handler(status int, location string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.seen = append(r.seen, req.Header.Clone())
		r.mu.Unlock()
		if location != "" {
			w.Header().Set("Location", location)
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}
}

func (r *r88provgHeaderRecorder) headers() []http.Header {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]http.Header(nil), r.seen...)
}

// r88provgRedirectPair starts a foreign server and an API server whose every
// request redirects (307) to the foreign server.
func r88provgRedirectPair(t *testing.T) (api, foreign *r88provgHeaderRecorder, apiURL string) {
	t.Helper()
	foreign = &r88provgHeaderRecorder{}
	foreignSrv := httptest.NewServer(foreign.handler(0, ""))
	t.Cleanup(foreignSrv.Close)
	api = &r88provgHeaderRecorder{}
	apiSrv := httptest.NewServer(api.handler(http.StatusTemporaryRedirect, foreignSrv.URL+"/landing"))
	t.Cleanup(apiSrv.Close)
	return api, foreign, apiSrv.URL
}

// TestR88provgMaaSNoCredentialsToOtherOrigins ports TS 977231e913
// google-vertex-maas-provider-node.test.ts "should not send credentials or
// custom headers to other origins": a foreign URL gets neither Authorization
// nor the custom header, and the token func is never called.
func TestR88provgMaaSNoCredentialsToOtherOrigins(t *testing.T) {
	foreign := &r88provgHeaderRecorder{}
	srv := httptest.NewServer(foreign.handler(0, ""))
	defer srv.Close()

	tokenCalls := 0
	tr := &maasAuthTransport{
		base: http.DefaultTransport,
		headersFunc: func(context.Context) (map[string]string, error) {
			return map[string]string{"X-Custom-Header": "custom-value"}, nil
		},
		authToken: func(context.Context) (string, error) {
			tokenCalls++
			return "mock-auth-token", nil
		},
		baseURL: "https://aiplatform.googleapis.com/v1/projects/test-project/locations/global/endpoints/openapi",
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/image.png", nil)
	req.Header.Set("Accept", "image/*")
	resp, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	got := foreign.headers()
	if len(got) != 1 {
		t.Fatalf("requests = %d, want 1", len(got))
	}
	if v := got[0].Get("Authorization"); v != "" {
		t.Fatalf("Authorization = %q, want none", v)
	}
	if v := got[0].Get("X-Custom-Header"); v != "" {
		t.Fatalf("X-Custom-Header = %q, want none", v)
	}
	if got[0].Get("Accept") != "image/*" {
		t.Fatalf("Accept = %q, want the caller's header unchanged", got[0].Get("Accept"))
	}
	if tokenCalls != 0 {
		t.Fatalf("token func called %d times, want 0", tokenCalls)
	}
}

// TestR88provgMaaSRedirectHopGetsNoCredentials is the Go-only case of
// 977231e913: net/http runs the transport once per redirect hop, so a
// same-origin POST that is redirected to another origin must not carry the
// token or the custom headers on the second hop.
func TestR88provgMaaSRedirectHopGetsNoCredentials(t *testing.T) {
	api, foreign, apiURL := r88provgRedirectPair(t)
	p := NewMaaS(MaaSConfig{
		Project: "test-project",
		BaseURL: apiURL,
		Headers: StaticHeaders(map[string]string{"X-Custom": "header"}),
		AuthToken: func(context.Context) (string, error) {
			return "secret-token", nil
		},
	})
	model, err := p.LanguageModel("test-model")
	if err != nil {
		t.Fatal(err)
	}
	// The foreign server answers {}, which is not a chat completion; only
	// the headers each hop carried matter here.
	_, _ = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})

	if h := api.headers(); len(h) != 1 || h[0].Get("Authorization") != "Bearer secret-token" || h[0].Get("X-Custom") != "header" {
		t.Fatalf("API hop headers = %v, want the token and custom header", h)
	}
	h := foreign.headers()
	if len(h) != 1 {
		t.Fatalf("foreign requests = %d, want 1", len(h))
	}
	if h[0].Get("Authorization") != "" || h[0].Get("X-Custom") != "" {
		t.Fatalf("foreign hop leaked credentials: Authorization=%q X-Custom=%q", h[0].Get("Authorization"), h[0].Get("X-Custom"))
	}
}

// TestR88provgAuthTransportRedirectHopGetsNoToken covers the credential
// transport class sweep (S2) for the Vertex OAuth transport: a redirect to
// another origin gets no bearer token, while a same-origin hop keeps it.
func TestR88provgAuthTransportRedirectHopGetsNoToken(t *testing.T) {
	api, foreign, apiURL := r88provgRedirectPair(t)
	tokenCalls := 0
	client := &http.Client{Transport: &vertexinternal.AuthTransport{
		Base: http.DefaultTransport,
		TokenFunc: func(context.Context) (string, error) {
			tokenCalls++
			return "vertex-token", nil
		},
	}}
	resp, err := client.Get(apiURL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if h := api.headers(); len(h) != 1 || h[0].Get("Authorization") != "Bearer vertex-token" {
		t.Fatalf("API hop headers = %v", h)
	}
	if h := foreign.headers(); len(h) != 1 || h[0].Get("Authorization") != "" {
		t.Fatalf("foreign hop Authorization = %q, want none", h[0].Get("Authorization"))
	}
	if tokenCalls != 1 {
		t.Fatalf("token calls = %d, want 1", tokenCalls)
	}

	// Same-origin redirect hops keep the token.
	same := &r88provgHeaderRecorder{}
	mux := http.NewServeMux()
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/b", http.StatusFound)
	})
	mux.HandleFunc("/b", same.handler(0, ""))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err = client.Get(srv.URL + "/a")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if h := same.headers(); len(h) != 1 || h[0].Get("Authorization") != "Bearer vertex-token" {
		t.Fatalf("same-origin hop headers = %v, want the token", h)
	}
}

// TestR88provgExpressTransportRedirectHopGetsNoKey covers the class sweep
// for the express-mode API key transport.
func TestR88provgExpressTransportRedirectHopGetsNoKey(t *testing.T) {
	api, foreign, apiURL := r88provgRedirectPair(t)
	client := &http.Client{Transport: &vertexExpressTransport{
		base:       http.DefaultTransport,
		apiKeyFunc: func(context.Context) (string, error) { return "express-key", nil },
	}}
	resp, err := client.Get(apiURL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if h := api.headers(); len(h) != 1 || h[0].Get("x-goog-api-key") != "express-key" {
		t.Fatalf("API hop headers = %v", h)
	}
	if h := foreign.headers(); len(h) != 1 || h[0].Get("x-goog-api-key") != "" {
		t.Fatalf("foreign hop x-goog-api-key = %q, want none", h[0].Get("x-goog-api-key"))
	}
}

// r88provgVertexTranscribe runs a Vertex Gemini transcription against a
// server answering with parts and returns the result and the request's
// generationConfig.
func r88provgVertexTranscribe(t *testing.T, parts string, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, string) {
	t.Helper()
	var mu sync.Mutex
	var genConfig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			GenerationConfig json.RawMessage `json:"generationConfig"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		genConfig = string(body.GenerationConfig)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":`+parts+`}}]}`)
	}))
	defer srv.Close()
	model := NewGeminiTranscriptionModel(newVertexTestProvider(t, srv.URL), ModelGemini35Transcribe)
	opts.Audio, opts.MimeType = []byte{1, 2}, "audio/wav"
	res, err := model.DoTranscribe(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	return res, genConfig
}

// Ports google-vertex-gemini-transcription-model.test.ts (TS 41465fd1b6):
// the word granularity sets wordTimestamp when unset, a disabling option
// wins with a warning, segments are unavailable (warning), Words duplicates
// the word segments, and a non-blank transcript without word timings warns.
func TestR88provgVertexGeminiTranscriptionWords(t *testing.T) {
	const withWords = `[{"audioTranscription":{"text":"Hello world","words":[{"word":"Hello","startOffset":"0s","endOffset":"0.5s"},{"word":"world","startOffset":"0.5s","endOffset":"1s"}]}}]`
	const noWords = `[{"audioTranscription":{"text":"Hello world"}}]`
	word := []string{provider.TranscriptionTimestampGranularityWord}

	res, gc := r88provgVertexTranscribe(t, withWords, &provider.TranscriptionOptions{TimestampGranularities: []string{"word", "segment"}})
	if gc != `{"audioTranscriptionConfig":{"wordTimestamp":true}}` {
		t.Fatalf("generationConfig = %s", gc)
	}
	if len(res.Words) != 2 || res.Words[1] != (types.TranscriptionWord{Text: "world", StartSecond: 0.5, EndSecond: 1}) || len(res.Segments) != 2 {
		t.Fatalf("words = %+v segments = %+v", res.Words, res.Segments)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Details != "Vertex Gemini transcription returns word timings and does not provide separate caption segments." {
		t.Fatalf("warnings = %+v", res.Warnings)
	}

	for _, ns := range []string{"googleVertex", "vertex", "google"} {
		res, gc = r88provgVertexTranscribe(t, noWords, &provider.TranscriptionOptions{TimestampGranularities: word,
			ProviderOptions: map[string]interface{}{ns: map[string]interface{}{"wordTimestamp": false}}})
		if gc != `{"audioTranscriptionConfig":{"wordTimestamp":false}}` || len(res.Warnings) != 1 ||
			res.Warnings[0].Details != "The provider wordTimestamp option is false and overrides the requested word timings." {
			t.Fatalf("%s: generationConfig %s warnings %+v", ns, gc, res.Warnings)
		}
	}

	res, _ = r88provgVertexTranscribe(t, noWords, &provider.TranscriptionOptions{TimestampGranularities: word})
	if res.Words != nil || len(res.Warnings) != 1 || res.Warnings[0].Type != "other" ||
		res.Warnings[0].Message != "The Vertex Gemini transcription response omitted the requested word timings." {
		t.Fatalf("omitted words: %+v / %+v", res.Words, res.Warnings)
	}
	res, _ = r88provgVertexTranscribe(t, `[{"audioTranscription":{"text":"  "}}]`, &provider.TranscriptionOptions{TimestampGranularities: word})
	if len(res.Warnings) != 0 {
		t.Fatalf("blank transcript warnings = %+v", res.Warnings)
	}
	// No granularity: request and result unchanged.
	res, gc = r88provgVertexTranscribe(t, noWords, &provider.TranscriptionOptions{})
	if gc != "" || len(res.Warnings) != 0 {
		t.Fatalf("no granularity: %s %+v", gc, res.Warnings)
	}
}

package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestMintRealtimeClientSecretWireFormat(t *testing.T) {
	var path string
	var headers http.Header
	var body map[string]interface{}
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		headers = r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"vcst_test","expiresAt":1893456000}`))
	}))
	defer closeServer()

	p, err := New(Config{
		APIKey:       "long-lived",
		BaseURL:      serverURL + "/v4/ai",
		TeamIDOrSlug: "team_1",
		Headers:      map[string]string{"X-Custom": "yes"},
	})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	expires := 60
	secret, err := p.MintRealtimeClientSecret(context.Background(), MintRealtimeClientSecretParams{
		ModelID:             "openai/gpt-realtime",
		ExpiresAfterSeconds: &expires,
	})
	if err != nil {
		t.Fatalf("MintRealtimeClientSecret error = %v", err)
	}
	if path != "/v1/realtime/client-secrets" {
		t.Fatalf("path = %q", path)
	}
	if headers.Get("Authorization") != "Bearer long-lived" || headers.Get("ai-gateway-auth-method") != "api-key" {
		t.Fatalf("auth headers = %#v", headers)
	}
	if headers.Get("x-vercel-ai-gateway-team") != "team_1" || headers.Get("X-Custom") != "yes" {
		t.Fatalf("headers = %#v", headers)
	}
	if body["model"] != "openai/gpt-realtime" || body["expiresIn"] != float64(60) {
		t.Fatalf("body = %#v", body)
	}
	if secret.Token != "vcst_test" || secret.ExpiresAt == nil || *secret.ExpiresAt != 1893456000 {
		t.Fatalf("secret = %#v", secret)
	}
	if _, ok := body["routeKind"]; ok {
		t.Fatalf("realtime mint should omit routeKind, body = %#v", body)
	}
}

// TestMintRealtimeClientSecretIncludesRouteKindForTranscription mirrors the
// TS test coverage for mintClientSecret's routeKind parameter (4bf9ac2): a
// transcription-bound mint sends routeKind:"transcription" in the body.
func TestMintRealtimeClientSecretIncludesRouteKindForTranscription(t *testing.T) {
	var body map[string]interface{}
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"vcst_transcribe"}`))
	}))
	defer closeServer()

	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	_, err = p.MintRealtimeClientSecret(context.Background(), MintRealtimeClientSecretParams{
		ModelID:   "openai/gpt-realtime-whisper",
		RouteKind: "transcription",
	})
	if err != nil {
		t.Fatalf("MintRealtimeClientSecret error = %v", err)
	}
	if body["routeKind"] != "transcription" {
		t.Fatalf("routeKind = %#v, want transcription", body["routeKind"])
	}
	if body["model"] != "openai/gpt-realtime-whisper" {
		t.Fatalf("model = %#v", body["model"])
	}
}

// TestGatewayGetTranscriptionToken mirrors the TS
// gateway.experimental_transcription.getToken() test coverage: it mints a
// transcription-bound secret (routeKind:"transcription") and returns the
// streaming transcription WebSocket URL for the model.
func TestGatewayGetTranscriptionToken(t *testing.T) {
	var body map[string]interface{}
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"vcst_get_transcription_token","expiresAt":1893456000}`))
	}))
	defer closeServer()

	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	expires := 120
	result, err := p.GetTranscriptionToken(context.Background(), "openai/gpt-realtime-whisper", &TranscriptionClientSecretOptions{ExpiresAfterSeconds: &expires})
	if err != nil {
		t.Fatalf("GetTranscriptionToken error = %v", err)
	}
	if body["routeKind"] != "transcription" || body["model"] != "openai/gpt-realtime-whisper" || body["expiresIn"] != float64(120) {
		t.Fatalf("body = %#v", body)
	}
	wantURL := "ws://" + serverURL[len("http://"):] + "/v4/ai/transcription-model?ai-model-id=openai%2Fgpt-realtime-whisper"
	if result.Token != "vcst_get_transcription_token" || result.URL != wantURL {
		t.Fatalf("result = %#v, want URL %q", result, wantURL)
	}
	if result.ExpiresAt == nil || *result.ExpiresAt != 1893456000 {
		t.Fatalf("ExpiresAt = %#v, want 1893456000", result.ExpiresAt)
	}
}

// TestGatewayGetTranscriptionTokenOmitsNullExpiresAt mirrors
// TestGatewayRealtimeGetTokenOmitsNullExpiresAt for the transcription token path.
func TestGatewayGetTranscriptionTokenOmitsNullExpiresAt(t *testing.T) {
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"vcst_transcription_null","expiresAt":null}`))
	}))
	defer closeServer()

	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	result, err := p.GetTranscriptionToken(context.Background(), "openai/gpt-realtime-whisper", nil)
	if err != nil {
		t.Fatalf("GetTranscriptionToken error = %v", err)
	}
	if result.Token != "vcst_transcription_null" || result.ExpiresAt != nil {
		t.Fatalf("result = %#v, want token with omitted expiresAt", result)
	}
}

// TestToGatewayTranscriptionURLEncodesModelID mirrors TS
// toGatewayTranscriptionUrl: http(s) is upgraded to ws(s) and the model id
// (which may contain a slash, e.g. a qualified "provider/model" id) survives
// query encoding.
func TestToGatewayTranscriptionURLEncodesModelID(t *testing.T) {
	got := ToGatewayTranscriptionURL("https://ai-gateway.vercel.sh/v4/ai", "openai/gpt-realtime-whisper")
	want := "wss://ai-gateway.vercel.sh/v4/ai/transcription-model?ai-model-id=openai%2Fgpt-realtime-whisper"
	if got != want {
		t.Fatalf("ToGatewayTranscriptionURL = %q, want %q", got, want)
	}

	gotHTTP := ToGatewayTranscriptionURL("http://localhost:8080/v4/ai", "spacexai/grok-stt")
	wantHTTP := "ws://localhost:8080/v4/ai/transcription-model?ai-model-id=spacexai%2Fgrok-stt"
	if gotHTTP != wantHTTP {
		t.Fatalf("ToGatewayTranscriptionURL (http) = %q, want %q", gotHTTP, wantHTTP)
	}
}

// TestGatewayExperimentalTranscriptionReturnsCallableModel mirrors the TS
// callable gateway.experimental_transcription(modelId) shape: it returns a
// usable TranscriptionModel bound to the given model id.
func TestGatewayExperimentalTranscriptionReturnsCallableModel(t *testing.T) {
	p, err := New(Config{APIKey: "k"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	model := p.ExperimentalTranscription("openai/gpt-realtime-whisper")
	if model.ModelID() != "openai/gpt-realtime-whisper" || model.Provider() != "gateway" {
		t.Fatalf("model = %#v", model)
	}
}

func TestGatewayRealtimeGetToken(t *testing.T) {
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"vcst_get_token"}`))
	}))
	defer closeServer()

	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	result, err := p.GetRealtimeToken(context.Background(), provider.RealtimeFactoryGetTokenOptions{Model: "openai/gpt-realtime"})
	if err != nil {
		t.Fatalf("GetRealtimeToken error = %v", err)
	}
	if result.Token != "vcst_get_token" || result.URL != "ws://"+serverURL[len("http://"):]+"/v4/ai/realtime-model?ai-model-id=openai%2Fgpt-realtime" {
		t.Fatalf("result = %#v", result)
	}
}

func TestGatewayRealtimeGetTokenOmitsNullExpiresAt(t *testing.T) {
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"vcst_null","expiresAt":null}`))
	}))
	defer closeServer()

	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	result, err := p.GetRealtimeToken(context.Background(), provider.RealtimeFactoryGetTokenOptions{Model: "openai/gpt-realtime"})
	if err != nil {
		t.Fatalf("GetRealtimeToken error = %v", err)
	}
	if result.Token != "vcst_null" || result.ExpiresAt != nil {
		t.Fatalf("result = %#v, want token with omitted expiresAt", result)
	}
}

func TestGatewayBaseURLTrimsTrailingSlashForRuntimeAndModelRoutes(t *testing.T) {
	var paths []string
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v4/ai/embedding-model":
			_, _ = w.Write([]byte(`{"embeddings":[[1]],"usage":{"tokens":1}}`))
		case "/v1/realtime/client-secrets":
			_, _ = w.Write([]byte(`{"token":"vcst_trim"}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer closeServer()

	p, err := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai/"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	if _, err := NewEmbeddingModel(p, "openai/text-embedding-3-small").DoEmbed(context.Background(), "x", nil); err != nil {
		t.Fatalf("DoEmbed error = %v", err)
	}
	if _, err := p.GetRealtimeToken(context.Background(), provider.RealtimeFactoryGetTokenOptions{Model: "openai/gpt-realtime"}); err != nil {
		t.Fatalf("GetRealtimeToken error = %v", err)
	}
	if len(paths) != 2 || paths[0] != "/v4/ai/embedding-model" || paths[1] != "/v1/realtime/client-secrets" {
		t.Fatalf("paths = %#v", paths)
	}
}

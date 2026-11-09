package xai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r89provbServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After-Ms", "10")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func r89provbAPIError(t *testing.T, err error) *providererrors.ProviderError {
	t.Helper()
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a ProviderError", err)
	}
	return pe
}

// TS xai-error.ts xaiFailedResponseHandler (createJsonErrorResponseHandler):
// every xAI model's API error is an APICallError with the response headers
// (retry-after-ms drives the retry delay), the body and the parsed data; a
// body matching no xaiErrorDataSchema member gives the status text; the
// responses shape gives "code: error".
func TestR89provbAPIErrorsKeepResponse(t *testing.T) {
	ctx := context.Background()
	check := func(name string, err error, wantStatus int, wantMessage string) {
		t.Helper()
		pe := r89provbAPIError(t, err)
		if pe.StatusCode != wantStatus || pe.Message != wantMessage || pe.ResponseHeaders["retry-after-ms"] != "10" || pe.ResponseBody == "" {
			t.Errorf("%s: error = status %d message %q headers %v body %q; want status %d message %q with headers and body",
				name, pe.StatusCode, pe.Message, pe.ResponseHeaders, pe.ResponseBody, wantStatus, wantMessage)
		}
	}

	srv := r89provbServer(t, 429, `{"error":{"message":"slow down","type":"rate_limit"}}`)
	_, err := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-imagine-image").DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "p"})
	check("image", err, 429, "slow down")
	if pe := r89provbAPIError(t, err); pe.Data == nil {
		t.Errorf("image: Data = nil, want the parsed body")
	}

	srv = r89provbServer(t, 502, `<html>bad gateway</html>`)
	_, err = NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-imagine-image").DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "p"})
	check("image non-schema body", err, 502, "Bad Gateway")

	srv = r89provbServer(t, 400, `{"code":"invalid_argument","error":"bad model"}`)
	lm, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).LanguageModel("grok-4")
	_, err = lm.DoGenerate(ctx, &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	check("responses", err, 400, "invalid_argument: bad model")

	srv = r89provbServer(t, 429, `{"error":"too many"}`)
	op, _ := json.Marshal(xaiVideoOperation{RequestID: "r1"})
	_, err = NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-imagine-video").DoStatus(ctx, &provider.VideoModelV3StatusOptions{Operation: op})
	check("video status", err, 429, "too many")

	srv = r89provbServer(t, 429, `{"error":"too many"}`)
	_, err = NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-imagine-video").DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p"}})
	check("video start", err, 429, "too many")
}

// provc CR (TS generate-video.ts useStartStatus: XaiVideoModel has no
// doGenerate): the model reports its v0.5.2 poll options to the core
// start/status flow.
func TestR89provbVideoStartStatusPollDefaults(t *testing.T) {
	m := NewVideoModel(New(Config{APIKey: "k"}), "grok-imagine-video")
	if i, to := m.VideoStartStatusPollDefaults(nil); i != 0 || to != 0 {
		t.Errorf("empty options = %d, %d; want 0, 0", i, to)
	}
	i, to := m.VideoStartStatusPollDefaults(map[string]interface{}{"xai": map[string]interface{}{"pollIntervalMs": 250, "pollTimeoutMs": 90000}})
	if i != 250 || to != 90000 {
		t.Errorf("poll options = %d, %d; want 250, 90000", i, to)
	}
}

// The video status poll URL is built from the base URL and the request id
// (TS getFromApi validateUrl: false). A cross-origin redirect of that poll
// must not carry the API key (fetch drops Authorization on a cross-origin
// redirect; net/http does the same).
func TestR89provbVideoStatusRedirectDropsCredentials(t *testing.T) {
	var gotAuth string
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"pending"}`))
	}))
	defer foreign.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(foreign.URL, "127.0.0.1", "localhost", 1)+"/elsewhere", http.StatusFound)
	}))
	defer origin.Close()
	op, _ := json.Marshal(xaiVideoOperation{RequestID: "r1"})
	if _, err := NewVideoModel(New(Config{APIKey: "secret", BaseURL: origin.URL}), "grok-imagine-video").DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op}); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "" {
		t.Fatalf("foreign hop got Authorization %q", gotAuth)
	}
}

// TS extractResponseHeaders: repeated response header lines are joined with
// ", " in the video result headers (keys stay Go canonical, S6).
func TestR89provbVideoHeadersJoinRepeatedLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Multi", "a")
		w.Header().Add("X-Multi", "b")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"pending"}`))
	}))
	defer srv.Close()
	op, _ := json.Marshal(xaiVideoOperation{RequestID: "r1"})
	res, err := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "grok-imagine-video").DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Response.Headers["X-Multi"]; got != "a, b" {
		t.Fatalf("X-Multi = %q, want %q", got, "a, b")
	}
}

// TS xai-transcription-model.ts: providerMetadata.xai.words is the
// zod-parsed word list, so a present `speaker: null` is kept.
func TestR89provbTranscriptionWordsKeepNullSpeaker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"a b","words":[{"text":"a","start":0,"end":1,"speaker":0},{"text":"b","start":1,"end":2,"speaker":null}]}`))
	}))
	defer srv.Close()
	res, err := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "").DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res.ProviderMetadata)
	if want := `{"xai":{"words":[{"end":1,"speaker":0,"start":0,"text":"a"},{"end":2,"speaker":null,"start":1,"text":"b"}]}}`; string(raw) != want {
		t.Fatalf("metadata = %s, want %s", raw, want)
	}
}

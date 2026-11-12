package bfl

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR91provcVideoPollSendsXKeyAndUserAgent (provc-01): TS doStatus sends
// combineHeaders(await resolve(config.headers), options.headers) to a trusted
// polling URL, and config.headers carries x-key and the provider user agent
// (black-forest-labs-video-model.ts:603, black-forest-labs-provider.ts:93).
func TestR91provcVideoPollSendsXKeyAndUserAgent(t *testing.T) {
	var mu sync.Mutex
	var pollKey, pollUA, pollCustom string
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/"+VideoModelFlux3Video, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "req-1", "polling_url": serverURL + "/get_result"})
	})
	mux.HandleFunc("/get_result", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		pollKey, pollUA, pollCustom = r.Header.Get("X-Key"), r.Header.Get("User-Agent"), r.Header.Get("X-Custom")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"Pending"}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL

	model := NewVideoModel(New(Config{APIKey: "secret-key", BaseURL: server.URL, Headers: map[string]string{"X-Custom": "cfg"}}), VideoModelFlux3Video)
	start, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"}})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	if _, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: start.Operation}); err != nil {
		t.Fatalf("DoStatus: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if pollKey != "secret-key" {
		t.Errorf("poll X-Key = %q, want secret-key", pollKey)
	}
	if !strings.Contains(pollUA, "ai-sdk-black-forest-labs/") {
		t.Errorf("poll User-Agent = %q, want the provider suffix", pollUA)
	}
	if pollCustom != "cfg" {
		t.Errorf("poll X-Custom = %q", pollCustom)
	}
}

// TestR91provcVideoPollMissingKey (provc-01): TS loadApiKey throws while the
// poll headers resolve, before any request.
func TestR91provcVideoPollMissingKey(t *testing.T) {
	t.Setenv("BFL_API_KEY", "")
	hit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer server.Close()
	model := NewVideoModel(New(Config{BaseURL: server.URL}), VideoModelFlux3Video)
	op, _ := json.Marshal(bflVideoOperation{RequestID: "r", PollingURL: server.URL + "/get_result"})
	_, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	var keyErr *providererrors.LoadAPIKeyError
	if !errors.As(err, &keyErr) {
		t.Fatalf("err = %T %v, want LoadAPIKeyError", err, err)
	}
	if hit {
		t.Error("a request was sent without a key")
	}
}

// TestR91provcVideoEmptyDetailKeepsSuffix (provc-09): TS describePollDetails
// returns "" for details "" and `detail != null` keeps the ": " suffix.
func TestR91provcVideoEmptyDetailKeepsSuffix(t *testing.T) {
	model, server := newVideoHTTPModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"Failed","details":""}`)
	})
	defer server.Close()
	op, _ := json.Marshal(bflVideoOperation{RequestID: "req-9", PollingURL: server.URL + "/get_result"})
	status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatal(err)
	}
	if want := `Black Forest Labs video generation failed with status "Failed": . Request id: req-9`; status.Error != want {
		t.Errorf("error = %q, want %q", status.Error, want)
	}
}

// TestR91provcVideoTimeoutIsVideoGenerationError (provc-10): TS throws
// AISDKError BLACK_FOREST_LABS_VIDEO_GENERATION_TIMEOUT.
func TestR91provcVideoTimeoutIsVideoGenerationError(t *testing.T) {
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/"+VideoModelFlux3Video, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "req-t", "polling_url": serverURL + "/get_result"})
	})
	mux.HandleFunc("/get_result", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"Pending"}`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL
	model := NewVideoModel(New(Config{APIKey: "k", BaseURL: server.URL, PollIntervalMillis: 5, PollTimeoutMillis: 1}), VideoModelFlux3Video)
	_, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "x"})
	var vErr *providererrors.VideoGenerationError
	if !errors.As(err, &vErr) {
		t.Fatalf("err = %T %v, want VideoGenerationError", err, err)
	}
	if want := "Black Forest Labs video generation timed out after 1ms. Request id: req-t"; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
}

// TestR91provcVideoJSNumbers (provc-11): TS template literals use String(n)
// for durations, and Number() parses an over-range resolution side.
func TestR91provcVideoJSNumbers(t *testing.T) {
	m := newVideoTestModel(t)
	d := 1000000.5
	_, warnings, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{Prompt: "x", Duration: &d})
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, w := range warnings {
		texts = append(texts, w.Details)
	}
	joined := strings.Join(texts, "\n")
	for _, want := range []string{
		"The requested duration of 1000000.5 was rounded to 1000001.",
		"The requested duration of 1000000.5 was clamped to 20.",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings %q lack %q", joined, want)
		}
	}

	tier, derived, ok := resolveTopLevelResolution("99999999999999999999x1080")
	if !ok || tier != "fhd" || derived {
		t.Errorf("resolution = %q derived=%v ok=%v, want fhd, not derived", tier, derived, ok)
	}
}

// TestR91provcVideoHeadersAndBodyOrder (provc-06, provc-07): repeated
// response headers are joined with ", " (TS extractResponseHeaders), and the
// submit body keeps the TS literal key order.
func TestR91provcVideoHeadersAndBodyOrder(t *testing.T) {
	var mu sync.Mutex
	var body string
	var serverURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/"+VideoModelFlux3Video, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = string(raw)
		mu.Unlock()
		w.Header().Add("X-Test", "a")
		w.Header().Add("X-Test", "b")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "req-1", "polling_url": serverURL + "/get_result"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	serverURL = server.URL
	model := NewVideoModel(New(Config{APIKey: "k", BaseURL: server.URL}), VideoModelFlux3Video)
	d := 6.0
	audio := true
	start, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
		Prompt: "x", Duration: &d, AspectRatio: "16:9", Resolution: "hd", GenerateAudio: &audio,
		ProviderOptions: map[string]interface{}{"blackForestLabs": map[string]interface{}{"safetyTolerance": 2, "draft": true}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := start.Response.Headers["X-Test"]; got != "a, b" {
		t.Errorf("X-Test = %q, want \"a, b\"", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := `{"mode":"t2v","prompt":"x","aspect_ratio":"16:9","duration":6,"resolution":"hd","generate_audio":true,"safety_tolerance":2,"draft":true}`; body != want {
		t.Errorf("body = %s\nwant %s", body, want)
	}
}

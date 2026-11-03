package xai

import (
	"context"
	"errors"
	"fmt"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS xai-model-catalog.ts modelsWithoutNoneReasoningEffort, evaluated by
// Node 24 at 30548ee7f9 (true = "none" is accepted).
func TestR85provbSupportsNoneReasoningEffortMatchesNode(t *testing.T) {
	want := map[string]bool{"grok-4": true, "grok-4.5": false, "grok-4.6": false, "grok-4.7": false, "grok-4.9-fast": false, "grok-4.20": true, "grok-4.20-reasoning": true, "grok-4.55": true, "grok-4.5x": false, "grok-5": false, "grok-5-mini": false, "grok-5.1": false, "grok-50": false, "grok-10": false, "grok-1": true, "grok-3-mini": true, "grok-0": true, "grok-5x": true, "grok-4-1-fast-reasoning": true, "grok-code-fast-1": true, "grok-4.6-2026": false, "grok-09": true}
	for id, w := range want {
		if got := supportsNoneReasoningEffort(id); got != w {
			t.Errorf("supportsNoneReasoningEffort(%q) = %v, want %v", id, got, w)
		}
	}
}

// TS 61a315e333: reasoning "none" on Grok 4.5+ maps to the lowest effort.
func TestR85provbGrokNoneReasoningMapsToLow(t *testing.T) {
	none := types.ReasoningNone
	for id, want := range map[string]string{"grok-4.7": "low", "grok-4": "none"} {
		m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), id)
		body, warnings, err := m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: &none}, false)
		if err != nil {
			t.Fatal(err)
		}
		r, _ := body["reasoning"].(map[string]interface{})
		if r["effort"] != want {
			t.Errorf("%s: effort = %v, want %s", id, r["effort"], want)
		}
		if want == "low" && (len(warnings) != 1 || warnings[0].Type != "compatibility" || warnings[0].Details != `reasoning "none" is not directly supported by this model. mapped to effort "low".`) {
			t.Errorf("%s: warnings = %#v", id, warnings)
		}
	}
}

// TestR85provbAPIKeyFunc: TS 7716e189ee async credentials. The APIKeyFunc is
// called for every request and its key is sent; the environment is not
// read; its error fails the request; APIKey plus APIKeyFunc is an
// InvalidArgumentError.
func TestR85provbAPIKeyFunc(t *testing.T) {
	t.Setenv("XAI_API_KEY", "env-key")
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"stop"}}`))
	}))
	defer srv.Close()

	calls := 0
	keyFunc := func(ctx context.Context) (string, error) {
		calls++
		return fmt.Sprintf("k%d", calls), nil
	}
	call := func(cfg Config) error {
		model, err := New(cfg).LanguageModel("grok-4")
		if err != nil {
			return err
		}
		_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		return err
	}
	cfg := Config{BaseURL: srv.URL}
	cfg.APIKeyFunc = keyFunc
	_ = call(cfg)
	_ = call(cfg)
	mu.Lock()
	got := append([]string(nil), seen...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "Bearer k1" || got[1] != "Bearer k2" {
		t.Fatalf("credential headers = %q, want Bearer k1, Bearer k2", got)
	}

	sentinel := errors.New("vault unavailable")
	cfg.APIKeyFunc = func(context.Context) (string, error) { return "", sentinel }
	if err := call(cfg); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the APIKeyFunc error", err)
	}

	cfg.APIKeyFunc = keyFunc
	cfg.APIKey = "static"
	err := call(cfg)
	var invalid *providererrors.InvalidArgumentError
	if !errors.As(err, &invalid) {
		t.Fatalf("both set: err = %v, want InvalidArgumentError", err)
	}
}

// R85 reviewer item 3: BOM-prefixed JSON bodies parse as in TS.
func TestR85provbBOMBodies(t *testing.T) {
	if got := parseXAIErrorMessage([]byte("\xEF\xBB\xBF{\"error\":{\"message\":\"bad\"}}")); got != "bad" {
		t.Errorf("error message = %q", got)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("\xEF\xBB\xBF{\"text\":\"hi\",\"duration\":1}"))
	}))
	defer srv.Close()
	m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).TranscriptionModel("grok-stt")
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"})
	if err != nil || res.Text != "hi" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

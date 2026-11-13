package typesafeai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Go-native replays of the TS tests the wire replay cannot carry (factory,
// cancellation and environment cases). See the round 42 report.

// TS: 'retains the deprecated factory alias'.
func TestProvider_RetainsDeprecatedEvaluationFactoryAlias(t *testing.T) {
	p := New(Config{})
	m, err := p.EvaluationModel("jev-latest")
	if err != nil {
		t.Fatalf("EvaluationModel: %v", err)
	}
	if m.Provider() != "typesafe.decision" || m.ModelID() != "jev-latest" {
		t.Fatalf("provider=%q model=%q", m.Provider(), m.ModelID())
	}
	if NewEvaluationModel(p, "jev-latest").Provider() != "typesafe.decision" {
		t.Fatal("NewEvaluationModel provider mismatch")
	}
}

// TS: 'rejects unsupported %s factories' expects NoSuchModelError.
func TestProvider_UnsupportedFactoriesAreNoSuchModelError(t *testing.T) {
	p := New(Config{})
	checks := map[string]error{}
	_, checks["languageModel"] = p.LanguageModel("unknown")
	_, checks["embeddingModel"] = p.EmbeddingModel("unknown")
	_, checks["imageModel"] = p.ImageModel("unknown")
	for name, err := range checks {
		var nsm *providererrors.NoSuchModelError
		if !errors.As(err, &nsm) || nsm.ModelID != "unknown" || nsm.ModelType != name {
			t.Errorf("%s: err = %v, want NoSuchModelError", name, err)
		}
	}
}

// TS: 'forwards cancellation to fetch' (one request, abort surfaces).
func TestDecisionModel_ForwardsCancellationToHTTP(t *testing.T) {
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body) // lets the server notice the client closing
		cancel()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer server.Close()

	model, _ := New(Config{APIKey: "test", BaseURL: server.URL}).DecisionModel("jev-latest")
	_, err := model.DoDecide(ctx, provider.DecisionCallOptions{State: "test", Questions: nativeQuestions()})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled in chain", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("requests = %d, want 1", calls.Load())
	}
}

func restoredAuth(t *testing.T, cfg Config) string {
	t.Helper()
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(nativeResponseJSON))
	}))
	defer server.Close()
	cfg.BaseURL = server.URL
	model, _ := New(cfg).DecisionModel("jev-latest")
	restored, err := deserializeModel(model.(*DecisionModel).Serialize())
	if err != nil {
		t.Fatalf("deserializeModel: %v", err)
	}
	if _, err := restored.DoDecide(t.Context(), provider.DecisionCallOptions{State: "test", Questions: nativeQuestions()}); err != nil {
		t.Fatalf("DoDecide: %v", err)
	}
	return gotAuth
}

// TS: 'restores authentication from the environment when no headers are
// serialized'.
func TestSerialization_RestoresAuthFromEnvironment(t *testing.T) {
	t.Setenv("TYPESAFE_AI_API_KEY", "workflow-key")
	if got := restoredAuth(t, Config{APIKey: "not-serialized"}); got != "Bearer workflow-key" {
		t.Fatalf("Authorization = %q", got)
	}
}

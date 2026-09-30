package googlevertex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Tests for the d20f0dc row: vertex.interactions() reuses the base google
// package's InteractionsLanguageModel (generalized via InteractionsConfig)
// with Vertex's OAuth client and location-scoped, endpoint-style base URL.

func TestVertexInteractions_ExpressModeRejected(t *testing.T) {
	p, err := New(Config{APIKey: "vertex-api-key"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := p.Interactions("gemini-omni-flash-preview"); err == nil {
		t.Fatal("expected an Express Mode error from Interactions()")
	}
	if _, err := p.InteractionsAgent("deep-research-pro-preview-12-2025"); err == nil {
		t.Fatal("expected an Express Mode error from InteractionsAgent()")
	}
	if _, err := p.InteractionsManagedAgent("my-agent"); err == nil {
		t.Fatal("expected an Express Mode error from InteractionsManagedAgent()")
	}
}

func TestVertexInteractions_EmptyIDErrors(t *testing.T) {
	p, err := New(Config{Project: "test-project", Location: "us-central1", AccessToken: "test-token"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := p.Interactions(""); err == nil {
		t.Fatal("expected an error for an empty model ID")
	}
	if _, err := p.InteractionsAgent(""); err == nil {
		t.Fatal("expected an error for an empty agent")
	}
	if _, err := p.InteractionsManagedAgent(""); err == nil {
		t.Fatal("expected an error for an empty managed agent id")
	}
}

// TestVertexInteractions_UsesLocationScopedBaseURLAndOAuth verifies the
// request hits "/interactions" (no "/publishers/google" segment — TS
// createConfig('interactions', { endpoint: true })) with a Vertex
// OAuth Bearer token, and correctly sends "model" (not "agent") for a
// plain model ID.
func TestVertexInteractions_UsesLocationScopedBaseURLAndOAuth(t *testing.T) {
	var gotPath, gotAuth string
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"v1_x","status":"completed","steps":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	p, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	model, err := p.Interactions("gemini-omni-flash-preview")
	if err != nil {
		t.Fatalf("Interactions() error = %v", err)
	}
	if model.Provider() != "google.vertex.interactions" {
		t.Fatalf("Provider() = %q, want google.vertex.interactions", model.Provider())
	}

	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	}); err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}

	if gotPath != "/interactions" {
		t.Fatalf("path = %q, want /interactions (no /publishers/google segment)", gotPath)
	}
	if strings.Contains(gotPath, "publishers/google") {
		t.Fatalf("path should not include publishers/google: %q", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization = %q, want a Vertex OAuth bearer token", gotAuth)
	}
	if body["model"] != "gemini-omni-flash-preview" {
		t.Fatalf("request body model = %v, want gemini-omni-flash-preview", body["model"])
	}
	if _, hasAgent := body["agent"]; hasAgent {
		t.Fatalf("request body should not include \"agent\" for a plain model id: %#v", body)
	}
}

// TestVertexInteractionsAgent verifies an agent-name model sends "agent"
// (not "model") in the request body, matching TS's agent branch.
func TestVertexInteractionsAgent(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"v1_a","status":"completed","steps":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	p, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	model, err := p.InteractionsAgent("deep-research-pro-preview-12-2025")
	if err != nil {
		t.Fatalf("InteractionsAgent() error = %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "research something"},
	}); err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if body["agent"] != "deep-research-pro-preview-12-2025" {
		t.Fatalf("request body agent = %v", body["agent"])
	}
	if _, hasModel := body["model"]; hasModel {
		t.Fatalf("request body should not include \"model\" for an agent call: %#v", body)
	}
}

// TestVertexInteractionsManagedAgent verifies a managed-agent id sends
// "agent" in the request body too (TS: identical wire behavior to a preset
// agent).
func TestVertexInteractionsManagedAgent(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"v1_m","status":"completed","steps":[{"type":"text","text":"ok"}]}`))
	}))
	defer server.Close()

	p, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	model, err := p.InteractionsManagedAgent("my-custom-agent")
	if err != nil {
		t.Fatalf("InteractionsManagedAgent() error = %v", err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
	}); err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if body["agent"] != "my-custom-agent" {
		t.Fatalf("request body agent = %v", body["agent"])
	}
}

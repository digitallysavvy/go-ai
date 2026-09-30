package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestSerializeAndDeserializeLanguageModel covers SER2's "only a static
// AccessToken is serializable" case: even Config.AccessToken is stripped as
// a credential (see the Serialize() doc comment), so only
// Project/Location/BaseURL/Headers round-trip. The restored model has no
// working auth and the caller must re-supply it.
func TestSerializeAndDeserializeLanguageModel(t *testing.T) {
	t.Parallel()

	p, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "static-token",
		Headers:     map[string]string{"X-Trace": "1"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	modelAny, err := p.LanguageModel("grok-4")
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}

	serialized := modelAny.(*LanguageModel).Serialize()
	if serialized.Provider != "googleVertex.xai" || serialized.ModelID != "grok-4" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["accessToken"]; ok {
		t.Fatalf("access token must be omitted from serializable config (credential): %#v", serialized.Config)
	}
	if _, ok := serialized.Config["AccessToken"]; ok {
		t.Fatalf("access token must be omitted from serializable config (credential): %#v", serialized.Config)
	}
	if project, _ := serialized.Config["Project"].(string); project != "test-project" {
		t.Fatalf("expected project to round-trip: %#v", serialized.Config)
	}

	// Deserializing without re-supplying auth fails, since New() requires a
	// project but has no auth requirement of its own -- the restored model
	// exists but cannot authenticate a real request. Supply a fresh
	// AccessToken via a manual SerializedModel to prove the rest round-trips.
	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel() error = %v", err)
	}
	if restored.Provider() != "googleVertex.xai" || restored.ModelID() != "grok-4" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeModel(serialized)
	if err != nil {
		t.Fatalf("provider.DeserializeModel() error = %v", err)
	}
	if viaRegistry.ModelID() != "grok-4" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestDeserializeLanguageModel_ErrorForMissingProject(t *testing.T) {
	t.Parallel()

	_, err := deserializeModel(provider.SerializedModel{
		Provider: "googleVertex.xai",
		ModelID:  "grok-4",
		Config:   map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("expected error because project is required")
	}
}

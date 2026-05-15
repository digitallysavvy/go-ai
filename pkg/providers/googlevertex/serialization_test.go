package googlevertex

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestVertexSerializeAndDeserializeModel(t *testing.T) {
	t.Parallel()

	p, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "token",
		BaseURL:     "https://example.test",
		Headers:     map[string]string{"X-Trace": "1"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	modelAny, err := p.LanguageModel("gemini-2.0-flash")
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}

	serialized := modelAny.(*LanguageModel).Serialize()
	if serialized.Provider != "google-vertex" {
		t.Fatalf("provider = %q, want google-vertex", serialized.Provider)
	}
	if serialized.ModelID != "gemini-2.0-flash" {
		t.Fatalf("modelID = %q", serialized.ModelID)
	}
	if serialized.Config == nil {
		t.Fatal("serialized config must not be nil")
	}
	if _, ok := serialized.Config["headers"]; !ok {
		t.Fatalf("expected serializable headers in config: %#v", serialized.Config)
	}
	if _, ok := serialized.Config["accessToken"]; ok {
		t.Fatalf("access token must be omitted from serializable config: %#v", serialized.Config)
	}

	restored, err := deserializeModel(serialized)
	if err == nil {
		t.Fatal("expected deserializeModel() to fail because serialized config omits auth tokens")
	}

	manual := provider.SerializedModel{
		Provider: "google-vertex",
		ModelID:  "gemini-2.0-flash",
		Config: map[string]interface{}{
			"project":     "test-project",
			"location":    "us-central1",
			"accessToken": "token",
			"baseURL":     "https://example.test",
		},
	}

	restored, err = deserializeModel(manual)
	if err != nil {
		t.Fatalf("deserializeModel(manual) error = %v", err)
	}
	if restored.Provider() != "google-vertex" || restored.ModelID() != "gemini-2.0-flash" {
		t.Fatalf("manual restored mismatch provider=%q model=%q", restored.Provider(), restored.ModelID())
	}

	restoredByRegistry, err := provider.DeserializeModel(manual)
	if err != nil {
		t.Fatalf("provider.DeserializeModel() error = %v", err)
	}
	if restoredByRegistry.Provider() != "google-vertex" {
		t.Fatalf("registry restored provider = %q", restoredByRegistry.Provider())
	}
}

func TestVertexDeserializeModel_ErrorForInvalidConfig(t *testing.T) {
	t.Parallel()

	_, err := deserializeModel(provider.SerializedModel{
		Provider: "google-vertex",
		ModelID:  "gemini-2.0-flash",
		Config:   map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("expected error because project/location/token are required")
	}
}

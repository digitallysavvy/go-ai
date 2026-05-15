package google

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestGoogleSerializeAndDeserializeModel(t *testing.T) {
	t.Parallel()

	p := New(Config{
		APIKey:  "k",
		BaseURL: "https://example.test/v1beta",
		Name:    "google.generative-ai",
		Headers: map[string]string{"X-Trace": "1"},
	})

	modelAny, err := p.LanguageModel(ModelGemini20Flash)
	if err != nil {
		t.Fatalf("LanguageModel() error = %v", err)
	}

	serialized := modelAny.(*LanguageModel).Serialize()
	if serialized.Provider != "google.generative-ai" {
		t.Fatalf("provider = %q, want google.generative-ai", serialized.Provider)
	}
	if serialized.ModelID != ModelGemini20Flash {
		t.Fatalf("modelID = %q", serialized.ModelID)
	}
	if serialized.Config == nil {
		t.Fatal("serialized config must not be nil")
	}
	if _, ok := serialized.Config["headers"]; !ok {
		t.Fatalf("expected serializable headers in config: %#v", serialized.Config)
	}
	if _, ok := serialized.Config["apiKey"]; ok {
		t.Fatalf("API key must be omitted from serializable config: %#v", serialized.Config)
	}

	restored, err := deserializeModel(serialized)
	if err != nil {
		t.Fatalf("deserializeModel() error = %v", err)
	}
	if restored.Provider() != "google.generative-ai" {
		t.Fatalf("restored provider = %q", restored.Provider())
	}
	if restored.ModelID() != ModelGemini20Flash {
		t.Fatalf("restored model ID = %q", restored.ModelID())
	}

	restoredByRegistry, err := provider.DeserializeModel(serialized)
	if err != nil {
		t.Fatalf("provider.DeserializeModel() error = %v", err)
	}
	if restoredByRegistry.Provider() != "google.generative-ai" {
		t.Fatalf("registry restored provider = %q", restoredByRegistry.Provider())
	}
}

func TestGoogleDeserializeModel_ErrorForEmptyModelID(t *testing.T) {
	t.Parallel()

	_, err := deserializeModel(provider.SerializedModel{
		Provider: "google",
		ModelID:  "",
		Config:   map[string]interface{}{"baseURL": "https://example.test"},
	})
	if err == nil {
		t.Fatal("expected error for empty model ID")
	}
}

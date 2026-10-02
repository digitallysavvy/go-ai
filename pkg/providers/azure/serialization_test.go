package azure

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestAzureSerializeAndDeserializeEmbeddingModel(t *testing.T) {
	p := mustNewProvider(t, Config{
		APIKey:       "k",
		ResourceName: "r",
		APIVersion:   "2024-10-21",
	})
	modelAny, err := p.EmbeddingModel("embed-dep")
	if err != nil {
		t.Fatalf("EmbeddingModel error = %v", err)
	}
	serialized := modelAny.(*EmbeddingModel).Serialize()
	if serialized.Provider != "azure.embeddings" || serialized.ModelID != "embed-dep" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}
	if _, ok := serialized.Config["APIKey"]; ok {
		t.Fatalf("serialized config should not include apiKey: %#v", serialized.Config)
	}

	restored, err := deserializeEmbeddingModel(serialized)
	if err != nil {
		t.Fatalf("deserializeEmbeddingModel error = %v", err)
	}
	if restored.Provider() != "azure.embeddings" || restored.ModelID() != "embed-dep" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeEmbeddingModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeEmbeddingModel error = %v", err)
	}
	if viaRegistry.ModelID() != "embed-dep" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestAzureSerializeAndDeserializeImageModel(t *testing.T) {
	p := mustNewProvider(t, Config{
		APIKey:       "k",
		ResourceName: "r",
		APIVersion:   "2024-10-21",
	})
	modelAny, err := p.ImageModel("dalle-dep")
	if err != nil {
		t.Fatalf("ImageModel error = %v", err)
	}
	serialized := modelAny.(*ImageModel).Serialize()
	if serialized.Provider != "azure.image" || serialized.ModelID != "dalle-dep" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeImageModel(serialized)
	if err != nil {
		t.Fatalf("deserializeImageModel error = %v", err)
	}
	if restored.Provider() != "azure.image" || restored.ModelID() != "dalle-dep" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeImageModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeImageModel error = %v", err)
	}
	if viaRegistry.ModelID() != "dalle-dep" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestAzureSerializeAndDeserializeSpeechModel(t *testing.T) {
	p := mustNewProvider(t, Config{
		APIKey:       "k",
		ResourceName: "r",
		APIVersion:   "2024-10-21",
	})
	modelAny, err := p.SpeechModel("tts-dep")
	if err != nil {
		t.Fatalf("SpeechModel error = %v", err)
	}
	serialized := modelAny.(*azureSpeechDispatchModel).Serialize()
	if serialized.Provider != "azure.speech" || serialized.ModelID != "tts-dep" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeSpeechModel(serialized)
	if err != nil {
		t.Fatalf("deserializeSpeechModel error = %v", err)
	}
	if restored.Provider() != "azure.speech" || restored.ModelID() != "tts-dep" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeSpeechModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeSpeechModel error = %v", err)
	}
	if viaRegistry.ModelID() != "tts-dep" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

func TestAzureSerializeAndDeserializeTranscriptionModel(t *testing.T) {
	p := mustNewProvider(t, Config{
		APIKey:       "k",
		ResourceName: "r",
		APIVersion:   "2024-10-21",
	})
	modelAny, err := p.TranscriptionModel("whisper-dep")
	if err != nil {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
	serialized := modelAny.(*azureTranscriptionDispatchModel).Serialize()
	if serialized.Provider != "azure.transcription" || serialized.ModelID != "whisper-dep" {
		t.Fatalf("serialize mismatch: %#v", serialized)
	}

	restored, err := deserializeTranscriptionModel(serialized)
	if err != nil {
		t.Fatalf("deserializeTranscriptionModel error = %v", err)
	}
	if restored.Provider() != "azure.transcription" || restored.ModelID() != "whisper-dep" {
		t.Fatalf("restored mismatch: provider=%s model=%s", restored.Provider(), restored.ModelID())
	}

	viaRegistry, err := provider.DeserializeTranscriptionModel(provider.SerializedModel{Provider: serialized.Provider, ModelID: serialized.ModelID, Config: serialized.Config})
	if err != nil {
		t.Fatalf("provider.DeserializeTranscriptionModel error = %v", err)
	}
	if viaRegistry.ModelID() != "whisper-dep" {
		t.Fatalf("registry restored mismatch: %#v", viaRegistry)
	}
}

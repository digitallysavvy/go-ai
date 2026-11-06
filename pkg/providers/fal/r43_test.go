package fal

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS parseProviderOptions treats providerOptions.fal === null as absent, so no
// schema defaults (language, version, batch_size) are applied.
func TestTranscriptionNullProviderOptionsAreAbsent(t *testing.T) {
	m := NewTranscriptionModel(New(Config{APIKey: "k"}), "wizper")
	body, err := m.buildRequestBody(&provider.TranscriptionOptions{ProviderOptions: map[string]interface{}{"fal": nil}})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"language", "version", "batch_size"} {
		if _, ok := body[k]; ok {
			t.Fatalf("%s must not be sent for null fal options: %#v", k, body)
		}
	}
	if body["chunk_level"] != "word" {
		t.Fatalf("chunk_level = %v", body["chunk_level"])
	}
}

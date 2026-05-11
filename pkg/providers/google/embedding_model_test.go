package google

import (
	"testing"
)

// TestGeminiEmbedding2PreviewModel verifies the embedding model ID constant exists
// and that the model routes to the correct API path.
func TestGeminiEmbedding2PreviewModel(t *testing.T) {
	if EmbeddingModelGeminiEmbedding2Preview != "gemini-embedding-2-preview" {
		t.Errorf("EmbeddingModelGeminiEmbedding2Preview = %q, want %q",
			EmbeddingModelGeminiEmbedding2Preview, "gemini-embedding-2-preview")
	}

	p := New(Config{APIKey: "test-key"})
	m := NewEmbeddingModel(p, EmbeddingModelGeminiEmbedding2Preview)

	if m.ModelID() != "gemini-embedding-2-preview" {
		t.Errorf("ModelID() = %q, want %q", m.ModelID(), "gemini-embedding-2-preview")
	}
	if m.Provider() != "google.generative-ai" {
		t.Errorf("Provider() = %q, want %q", m.Provider(), "google.generative-ai")
	}
}

// TestGeminiMultimodalEmbedding verifies that buildEmbeddingAPIParts correctly
// serializes text and image EmbeddingPart values to the Google API format.
func TestGeminiMultimodalEmbedding(t *testing.T) {
	imageBytes := []byte{0xFF, 0xD8, 0xFF} // minimal JPEG header

	parts := []EmbeddingPart{
		ImageEmbeddingPart{MimeType: "image/jpeg", Data: imageBytes},
	}

	apiParts := buildEmbeddingAPIParts("describe this image", parts)

	if len(apiParts) != 2 {
		t.Fatalf("expected 2 API parts (text + image), got %d", len(apiParts))
	}

	// First part must be the text.
	if apiParts[0]["text"] != "describe this image" {
		t.Errorf("apiParts[0].text = %v, want %q", apiParts[0]["text"], "describe this image")
	}

	// Second part must be the inlineData with correct MIME type.
	inlineData, ok := apiParts[1]["inlineData"].(map[string]interface{})
	if !ok {
		t.Fatalf("apiParts[1].inlineData is not a map, got %T", apiParts[1]["inlineData"])
	}
	if inlineData["mimeType"] != "image/jpeg" {
		t.Errorf("inlineData.mimeType = %v, want %q", inlineData["mimeType"], "image/jpeg")
	}
	if inlineData["data"] == "" {
		t.Error("inlineData.data must not be empty")
	}
}

// TestEmbeddingPartTextOnly verifies buildEmbeddingAPIParts with only a text part.
func TestEmbeddingPartTextOnly(t *testing.T) {
	parts := []EmbeddingPart{
		TextEmbeddingPart{Text: "extra context"},
	}

	apiParts := buildEmbeddingAPIParts("primary text", parts)

	if len(apiParts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(apiParts))
	}
	if apiParts[0]["text"] != "primary text" {
		t.Errorf("part[0].text = %v, want %q", apiParts[0]["text"], "primary text")
	}
	if apiParts[1]["text"] != "extra context" {
		t.Errorf("part[1].text = %v, want %q", apiParts[1]["text"], "extra context")
	}
}

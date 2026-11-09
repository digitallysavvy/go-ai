package baseten

import "testing"

// TestR90provcURLErrorsUseTSText: baseten-provider.ts createChatModel /
// createEmbeddingModel throw new Error with these texts; a /sync/v1 URL is
// used for chat even when it also contains /predict.
func TestR90provcURLErrorsUseTSText(t *testing.T) {
	_, err := New(Config{APIKey: "k", ModelURL: "https://model-x.api.baseten.co/environments/production/predict"}).LanguageModel("m")
	if want := "Not supported. You must use a /sync/v1 endpoint for chat models."; err == nil || err.Error() != want {
		t.Errorf("chat /predict err = %v, want %q", err, want)
	}
	if _, err := New(Config{APIKey: "k", ModelURL: "https://model-x.api.baseten.co/predict/sync/v1"}).LanguageModel("m"); err != nil {
		t.Errorf("chat /sync/v1 err = %v, want nil", err)
	}
	_, err = New(Config{APIKey: "k"}).EmbeddingModel("e")
	if want := "No model URL provided for embeddings. Please set modelURL option for embeddings."; err == nil || err.Error() != want {
		t.Errorf("embedding without URL err = %v, want %q", err, want)
	}
	_, err = New(Config{APIKey: "k", ModelURL: "https://model-x.api.baseten.co/environments/production/predict"}).EmbeddingModel("e")
	if want := "Not supported. You must use a /sync or /sync/v1 endpoint for embeddings."; err == nil || err.Error() != want {
		t.Errorf("embedding /predict err = %v, want %q", err, want)
	}
}

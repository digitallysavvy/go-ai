package groq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR84provaTextTranscriptionTextDecoder: TS
// groqTextTranscriptionResponseHandler decodes the body with TextDecoder: a
// leading BOM is dropped and a truncated UTF-8 sequence is one U+FFFD, in
// both text and response.body.
func TestR84provaTextTranscriptionTextDecoder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("\xEF\xBB\xBFhi\xE2\x82"))
	}))
	defer server.Close()
	model := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: server.URL}), "whisper-large-v3-turbo")
	result, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte("a"), MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"groq": map[string]interface{}{"responseFormat": "text"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = "hi�"
	if result.Text != want || result.Response.Body != want {
		t.Errorf("text = %q, body = %q, want %q", result.Text, result.Response.Body, want)
	}
}

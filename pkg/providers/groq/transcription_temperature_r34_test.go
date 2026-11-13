package groq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// A temperature that passes option validation (any Go numeric kind) must be
// forwarded, not silently dropped.
func TestGroqTranscriptionTemperatureNumericKindsForwarded(t *testing.T) {
	for name, temp := range map[string]interface{}{"float32": float32(0.5), "int64": int64(1), "int": 0, "float64": 0.25} {
		var form map[string][]string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseMultipartForm(10 << 20)
			form = r.MultipartForm.Value
			_, _ = w.Write([]byte(`{"text":"x","x_groq":{"id":"r"}}`))
		}))
		model := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: server.URL}), "whisper-large-v3")
		_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
			Audio: []byte("a"), MimeType: "audio/wav",
			ProviderOptions: map[string]interface{}{"groq": map[string]interface{}{"temperature": temp}},
		})
		server.Close()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if v := form["temperature"]; len(v) != 1 {
			t.Errorf("%s: temperature form value = %v, want exactly one", name, v)
		}
	}
}

package groq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS appends every option that is not undefined, so empty strings are sent.
func TestGroqTranscriptionEmptyStringOptionsSentR33(t *testing.T) {
	var form map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(10 << 20)
		form = r.MultipartForm.Value
		_, _ = w.Write([]byte(`{"text":"x","x_groq":{"id":"r"}}`))
	}))
	defer server.Close()
	model := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: server.URL}), "whisper-large-v3")
	_, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte("a"), MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"groq": map[string]interface{}{"language": "", "prompt": "", "responseFormat": ""}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"language", "prompt", "response_format"} {
		if v, ok := form[k]; !ok || len(v) != 1 || v[0] != "" {
			t.Errorf("%s = %v (present=%v), want one empty value", k, v, ok)
		}
	}
}

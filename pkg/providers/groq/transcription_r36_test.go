package groq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS reads currentDate before sending the request and formats temperature
// with String(number) (1e-7, not 0.0000001).
func TestGroqTranscriptionTimestampBeforeRequestAndJSTemperature(t *testing.T) {
	var form map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		_ = r.ParseMultipartForm(10 << 20)
		form = r.MultipartForm.Value
		_, _ = w.Write([]byte(`{"text":"x","x_groq":{"id":"r"}}`))
	}))
	defer server.Close()
	model := NewTranscriptionModel(New(Config{APIKey: "k", BaseURL: server.URL}), "whisper-large-v3")
	start := time.Now()
	res, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte("a"), MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"groq": map[string]interface{}{"temperature": 1e-7}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Response.Timestamp.After(start.Add(100 * time.Millisecond)) {
		t.Errorf("timestamp %v taken after the request, want before", res.Response.Timestamp.Sub(start))
	}
	if v := form["temperature"]; len(v) != 1 || v[0] != "1e-7" {
		t.Errorf("temperature = %v, want [1e-7]", v)
	}
}

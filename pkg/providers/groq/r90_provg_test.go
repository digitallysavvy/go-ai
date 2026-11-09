package groq

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR90provgTranscriptionDefaultMediaType: TS groq-transcription-model.ts
// sends the audio as audio.wav with type audio/wav when no mediaType is
// given (mediaType ?? 'audio/wav').
func TestR90provgTranscriptionDefaultMediaType(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"hi","x_groq":{"id":"x"}}`)
	}))
	defer server.Close()
	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model, _ := p.TranscriptionModel("whisper-large-v3")
	if _, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1, 2, 3}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `filename="audio.wav"`) || !strings.Contains(body, "Content-Type: audio/wav") {
		t.Fatalf("file part = %q, want audio.wav with type audio/wav", body)
	}
}

// TestR90provgTranscriptionTemperatureNumber: temperature is
// z.number().min(0).max(1): every Go integer kind is a number; NaN and
// Infinity are not.
func TestR90provgTranscriptionTemperatureNumber(t *testing.T) {
	for _, v := range []interface{}{int8(1), uint16(0), float32(0.5)} {
		if err := validateGroqTranscriptionOptions(map[string]interface{}{"groq": map[string]interface{}{"temperature": v}}); err != nil {
			t.Errorf("temperature %T(%v) rejected: %v", v, v, err)
		}
	}
	for _, v := range []interface{}{math.NaN(), math.Inf(1)} {
		if err := validateGroqTranscriptionOptions(map[string]interface{}{"groq": map[string]interface{}{"temperature": v}}); err == nil {
			t.Errorf("temperature %v accepted", v)
		}
	}
}

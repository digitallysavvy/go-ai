package xai

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS appendXaiSttParams writes String(value): a small vadThreshold is
// "1e-7" (JS number text), as the streaming query already sends it.
func TestR92provbTranscriptionVadThresholdNumberText(t *testing.T) {
	m := NewTranscriptionModel(New(Config{APIKey: "k"}), "stt")
	body, _, err := m.buildMultipartBody(&provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"vadThreshold": 1e-7}}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(body)
	if !strings.Contains(string(b), "\r\n\r\n1e-7\r\n") {
		t.Fatalf("body = %q", b)
	}
}

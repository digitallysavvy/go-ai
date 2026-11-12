package xai

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

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

// TS safeParseJSON reports any JSON message as a raw part, an object whose
// fields do not fit the event shape and a non-object value included.
func TestR92provbTranscriptionStreamRawAnyJSON(t *testing.T) {
	server := newXAISTTTestServer(t)
	defer server.close()
	model := newTestXAITranscriptionModel(server.ts.URL)
	result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
		Audio:            newXAIChanAudioStream([]byte{1}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		IncludeRawChunks: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck
	partsCh := make(chan []provider.TranscriptionStreamPart, 1)
	go func() {
		parts, _ := drainXAIStream(result.Stream)
		partsCh <- parts
	}()
	server.send(5.0)
	server.send(map[string]interface{}{"type": 7.0})
	server.send(map[string]interface{}{"type": "transcript.created"})
	server.waitForFrame(t, "audio.done", time.Second)
	server.send(map[string]interface{}{"type": "transcript.done", "text": "x", "duration": 1})
	var raws []interface{}
	for _, p := range <-partsCh {
		if p.Type == provider.TranscriptionStreamPartTypeRaw {
			raws = append(raws, p.RawValue)
		}
	}
	if len(raws) != 4 || raws[0] != 5.0 {
		t.Fatalf("raw parts = %v, want 4 starting with 5", raws)
	}
}

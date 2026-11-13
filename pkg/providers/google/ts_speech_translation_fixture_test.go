package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS "google-live-speech-translation.chunks.txt": the recorded live
// translation messages replayed over the WebSocket.
func TestSpeechTranslationModel_RecordedFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "ts", "speech-translation", "google-live-speech-translation.chunks.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var messages []map[string]interface{}
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("fixture line %q: %v", l, err)
		}
		messages = append(messages, m)
	}

	server := newLiveTranscriptionTestServer(t)
	defer server.close()
	model := newSpeechTranslationTestModel(t, server.ts.URL, time.Nanosecond) // TS finishGraceMs: 0
	result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
		Audio:            newChanAudioStream([]byte{1, 2, 3}),
		InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		TargetLanguage:   "es",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer result.Stream.Close() //nolint:errcheck

	// Drain concurrently: the stream emits parts (stream-start first) only
	// as they are read, and it handles setupComplete only after that.
	type drained struct {
		parts []*provider.SpeechTranslationStreamPart
		err   error
	}
	done := make(chan drained, 1)
	go func() {
		parts, err := drainUntilTranslationFinishOrError(t, result.Stream)
		done <- drained{parts, err}
	}()

	server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, 10*time.Second)
	// The trailing silence only finishes the stream once the end of the
	// input audio is known (TS reads the audio to its end before the replayed
	// messages arrive). Send setupComplete, which releases the audio pump,
	// and wait for audioStreamEnd before replaying the rest; otherwise a
	// loaded run can read the final silence first and wait forever.
	if _, ok := messages[0]["setupComplete"]; !ok {
		t.Fatalf("fixture must start with setupComplete, got %v", messages[0])
	}
	server.toSend <- messages[0]
	server.waitFor(t, hasRealtimeInputKey("audioStreamEnd"), 10*time.Second)
	for _, m := range messages[1:] {
		server.toSend <- m
	}
	var d drained
	select {
	case d = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("stream did not finish")
	}
	parts, err := d.parts, d.err
	if err != nil {
		t.Fatal(err)
	}

	var audio []string
	for _, p := range parts {
		if p.Type == provider.SpeechTranslationStreamPartTypeAudio {
			audio = append(audio, base64.StdEncoding.EncodeToString(p.AudioData))
			if p.ID != "google-item-0" {
				t.Fatalf("audio id = %q", p.ID)
			}
		}
	}
	if strings.Join(audio, ",") != "AQIDBAUGBwg=,AAAAAAAAAAAAAAAA" {
		t.Fatalf("audio = %v", audio)
	}
	n := len(parts)
	src, out, fin := parts[n-3], parts[n-2], parts[n-1]
	if src.Type != provider.SpeechTranslationStreamPartTypeSourceTranscriptFinal || src.ID != "google-item-0" || src.Text != "The quick brown fox jumps over the lazy dog." {
		t.Fatalf("source final = %+v", src)
	}
	if out.Type != provider.SpeechTranslationStreamPartTypeOutputTextFinal || out.ID != "google-item-0" || out.Text != "El zorro marrón rápido salta sobre el perro perezoso." {
		t.Fatalf("output final = %+v", out)
	}
	if fin.Type != provider.SpeechTranslationStreamPartTypeFinish || fin.SourceText != src.Text || fin.OutputText != out.Text ||
		fin.Usage == nil || *fin.Usage.InputAudioTokens != 150 || *fin.Usage.OutputAudioTokens != 150 {
		t.Fatalf("finish = %+v usage %+v", fin, fin.Usage)
	}
}

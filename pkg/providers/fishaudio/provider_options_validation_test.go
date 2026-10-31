package fishaudio

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS parseProviderOptions(fishAudio*ModelOptionsSchema): a mistyped or
// out-of-range option is an InvalidArgumentError before any request.
func TestProviderOptionsValidation(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"})
	speech, _ := p.SpeechModel("s2.1-pro")
	asr, _ := p.TranscriptionModel("default")
	badSpeech := []map[string]interface{}{
		{"temperature": 2},
		{"sampleRate": 1.5},
		{"mp3Bitrate": 100},
		{"opusBitrate": 1},
		{"latency": "slow"},
		{"chunkLength": 50},
		{"referenceId": 3},
		{"features": []interface{}{1}},
		{"volume": nil},
	}
	for _, o := range badSpeech {
		_, err := speech.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi", ProviderOptions: map[string]interface{}{"fishAudio": o}})
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) {
			t.Errorf("speech %v: error = %v, want InvalidArgumentError", o, err)
		}
	}
	for _, o := range []map[string]interface{}{{"language": 1}, {"ignoreTimestamps": "no"}} {
		_, err := asr.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg", ProviderOptions: map[string]interface{}{"fishAudio": o}})
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) {
			t.Errorf("transcription %v: error = %v, want InvalidArgumentError", o, err)
		}
	}
	if err := validateSpeechOptions(map[string]interface{}{"fishAudio": map[string]interface{}{
		"referenceId": []interface{}{"a", "b"}, "mp3Bitrate": 128, "opusBitrate": -1000, "chunkLength": 200,
	}}); err != nil {
		t.Errorf("valid options rejected: %v", err)
	}
	if err := validateSpeechOptions(map[string]interface{}{"fishAudio": SpeechModelOptions{}}); err != nil {
		t.Errorf("typed options rejected: %v", err)
	}
}

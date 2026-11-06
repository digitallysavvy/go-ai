package cartesia

import "testing"

func TestCartesiaOptionsAreValidated(t *testing.T) {
	badSpeech := []map[string]interface{}{
		{"container": "ogg"}, {"encoding": "pcm_x"}, {"sampleRate": 12345}, {"bitRate": 1}, {"speed": 2.0}, {"speed": 0.5}, {"language": 1},
	}
	for i, o := range badSpeech {
		if validateSpeechOptions(map[string]interface{}{"cartesia": o}) == nil {
			t.Fatalf("speech case %d (%v) accepted", i, o)
		}
	}
	if err := validateSpeechOptions(map[string]interface{}{"cartesia": map[string]interface{}{
		"container": "mp3", "sampleRate": 44100, "bitRate": 128000, "speed": 1.5, "encoding": nil,
	}}); err != nil {
		t.Fatalf("valid speech options rejected: %v", err)
	}
	if err := validateSpeechOptions(map[string]interface{}{"cartesia": SpeechModelOptions{Container: "wav"}}); err != nil {
		t.Fatalf("typed options rejected: %v", err)
	}
	badTr := []map[string]interface{}{
		{"timestampGranularities": []interface{}{"sentence"}}, {"streaming": map[string]interface{}{"encoding": "pcm_x"}},
		{"streaming": map[string]interface{}{"turnDetection": "yes"}}, {"streaming": nil}, {"language": 5},
	}
	for i, o := range badTr {
		if validateTranscriptionOptions(map[string]interface{}{"cartesia": o}) == nil {
			t.Fatalf("transcription case %d (%v) accepted", i, o)
		}
	}
	if err := validateTranscriptionOptions(map[string]interface{}{"cartesia": map[string]interface{}{
		"timestampGranularities": []interface{}{"word"}, "streaming": map[string]interface{}{"encoding": "pcm_s32le", "turnDetection": false},
	}}); err != nil {
		t.Fatalf("valid transcription options rejected: %v", err)
	}
}

package deepgram

import "testing"

func TestDeepgramOptionsAreValidated(t *testing.T) {
	badSpeech := []map[string]interface{}{
		{"bitRate": true}, {"container": 1}, {"sampleRate": "24000"}, {"callback": "no url"},
		{"callbackMethod": "GET"}, {"mipOptOut": "yes"}, {"tag": []interface{}{1}},
	}
	for i, o := range badSpeech {
		if validateSpeechOptions(map[string]interface{}{"deepgram": o}) == nil {
			t.Fatalf("speech case %d (%v) accepted", i, o)
		}
	}
	if err := validateSpeechOptions(map[string]interface{}{"deepgram": map[string]interface{}{
		"bitRate": 48000, "tag": []interface{}{"a"}, "callback": "https://x.test/cb", "callbackMethod": "PUT", "sampleRate": nil,
	}}); err != nil {
		t.Fatalf("valid speech options rejected: %v", err)
	}

	badTr := []map[string]interface{}{
		{"language": 1}, {"detectLanguage": "yes"}, {"summarize": "v3"}, {"summarize": true}, {"redact": 5}, {"uttSplit": "x"}, {"diarize": 1},
	}
	for i, o := range badTr {
		if validateTranscriptionOptions(map[string]interface{}{"deepgram": o}) == nil {
			t.Fatalf("transcription case %d (%v) accepted", i, o)
		}
	}
	if err := validateTranscriptionOptions(map[string]interface{}{"deepgram": map[string]interface{}{
		"summarize": false, "redact": []interface{}{"pci", "ssn"}, "uttSplit": 0.8, "language": nil,
	}}); err != nil {
		t.Fatalf("valid transcription options rejected: %v", err)
	}
}

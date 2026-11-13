package hume

import "testing"

func TestHumeSpeechOptionsAreValidated(t *testing.T) {
	bad := []map[string]interface{}{
		{"context": "x"},
		{"context": map[string]interface{}{}},
		{"context": map[string]interface{}{"generationId": 5}},
		{"context": map[string]interface{}{"utterances": "x"}},
		{"context": map[string]interface{}{"utterances": []interface{}{map[string]interface{}{"speed": 1}}}},
		{"context": map[string]interface{}{"utterances": []interface{}{map[string]interface{}{"text": "a", "speed": "fast"}}}},
		{"context": map[string]interface{}{"utterances": []interface{}{map[string]interface{}{"text": "a", "voice": map[string]interface{}{"id": "v", "provider": "OTHER"}}}}},
		{"context": map[string]interface{}{"utterances": []interface{}{map[string]interface{}{"text": "a", "voice": map[string]interface{}{"provider": "HUME_AI"}}}}},
	}
	for i, opts := range bad {
		if err := validateSpeechOptions(map[string]interface{}{"hume": opts}); err == nil {
			t.Fatalf("case %d (%v) accepted", i, opts)
		}
	}
	good := []map[string]interface{}{
		{"context": nil},
		{"context": map[string]interface{}{"generationId": "g"}},
		{"context": map[string]interface{}{"utterances": []interface{}{
			map[string]interface{}{"text": "a", "description": "calm", "speed": 1.2, "trailingSilence": 0.5, "voice": map[string]interface{}{"name": "n", "provider": "CUSTOM_VOICE"}},
			map[string]interface{}{"text": "b", "voice": map[string]interface{}{"id": "v"}},
		}}},
	}
	for i, opts := range good {
		if err := validateSpeechOptions(map[string]interface{}{"hume": opts}); err != nil {
			t.Fatalf("good case %d rejected: %v", i, err)
		}
	}
}

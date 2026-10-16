package gladia

import "testing"

func TestGladiaOptionsAreValidated(t *testing.T) {
	bad := []map[string]interface{}{
		{"contextPrompt": 1},
		{"customVocabulary": "x"},
		{"customVocabularyConfig": map[string]interface{}{}},
		{"customVocabularyConfig": map[string]interface{}{"vocabulary": []interface{}{map[string]interface{}{"intensity": 1}}}},
		{"callbackConfig": map[string]interface{}{"method": "POST"}},
		{"callbackConfig": map[string]interface{}{"url": "u", "method": "GET"}},
		{"subtitlesConfig": map[string]interface{}{"formats": []interface{}{"ass"}}},
		{"subtitlesConfig": map[string]interface{}{"style": "fancy"}},
		{"translationConfig": map[string]interface{}{"model": "base"}},
		{"summarizationConfig": map[string]interface{}{"type": "long"}},
		{"customSpellingConfig": map[string]interface{}{"spellingDictionary": map[string]interface{}{"a": "b"}}},
		{"audioToLlmConfig": map[string]interface{}{"prompts": "x"}},
		{"diarizationConfig": map[string]interface{}{"enhanced": "yes"}},
		{"customMetadata": "x"},
	}
	for i, o := range bad {
		if validateOptions(map[string]interface{}{"gladia": o}) == nil {
			t.Fatalf("case %d (%v) accepted", i, o)
		}
	}
	good := map[string]interface{}{
		"contextPrompt": nil, "customVocabulary": []interface{}{"a", map[string]interface{}{"x": 1}},
		"customVocabularyConfig": map[string]interface{}{"vocabulary": []interface{}{"a", map[string]interface{}{"value": "b", "intensity": 0.5}}, "defaultIntensity": nil},
		"callbackConfig":         map[string]interface{}{"url": "https://x.test", "method": "PUT"},
		"translationConfig":      map[string]interface{}{"targetLanguages": []interface{}{"fr"}, "model": "enhanced"},
		"customSpellingConfig":   map[string]interface{}{"spellingDictionary": map[string]interface{}{"a": []interface{}{"b"}}},
		"customMetadata":         map[string]interface{}{"k": []interface{}{1}},
		"subtitlesConfig":        map[string]interface{}{"formats": []interface{}{"srt", "vtt"}, "style": "compliance"},
	}
	if err := validateOptions(map[string]interface{}{"gladia": good}); err != nil {
		t.Fatalf("valid options rejected: %v", err)
	}
}

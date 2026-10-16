package elevenlabs

import "testing"

// elevenLabsSpeechModelOptionsSchema members are .optional(): an explicit
// null fails, a typed locator list is fine, bounds are enforced.
func TestSpeechProviderOptionsValidationNullAndBounds(t *testing.T) {
	bad := []map[string]interface{}{
		{"languageCode": nil},
		{"voiceSettings": map[string]interface{}{"stability": nil}},
		{"voiceSettings": map[string]interface{}{"style": 1.5}},
		{"seed": -1},
		{"previousRequestIds": []interface{}{"a", "b", "c", "d"}},
		{"pronunciationDictionaryLocators": []interface{}{map[string]interface{}{"versionId": "v"}}},
		{"applyTextNormalization": "maybe"},
		{"enableLogging": "yes"},
	}
	for _, o := range bad {
		if err := validateSpeechProviderOptions(map[string]interface{}{"elevenlabs": o}); err == nil {
			t.Errorf("%v accepted", o)
		}
	}
	good := []map[string]interface{}{
		{},
		{"languageCode": "en", "seed": 5, "voiceSettings": map[string]interface{}{"stability": 0.5, "useSpeakerBoost": true}},
		{"pronunciationDictionaryLocators": []PronunciationDictionaryLocator{{PronunciationDictionaryID: "d"}}},
		{"pronunciationDictionaryLocators": []interface{}{map[string]interface{}{"pronunciationDictionaryId": "d"}}},
	}
	for _, o := range good {
		if err := validateSpeechProviderOptions(map[string]interface{}{"elevenlabs": o}); err != nil {
			t.Errorf("%v rejected: %v", o, err)
		}
	}
}

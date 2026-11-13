package assemblyai

import "testing"

func TestAssemblyAIOptionsAreValidated(t *testing.T) {
	bad := []map[string]interface{}{
		{"audioEndAt": 1.5}, {"contentSafetyConfidence": 10}, {"contentSafetyConfidence": 101},
		{"customSpelling": []interface{}{map[string]interface{}{"to": "x"}}},
		{"keytermsPrompt": "a"}, {"speechThreshold": 2}, {"temperature": -1},
		{"removeAudioTags": "none"}, {"redactPiiAudioOptions": map[string]interface{}{"overrideAudioRedactionMethod": "beep"}},
		{"languageDetectionOptions": map[string]interface{}{"codeSwitchingConfidenceThreshold": 2}},
		{"redactStaticEntities": map[string]interface{}{"a": "b"}}, {"speakersExpected": "2"}, {"punctuate": 1},
	}
	for i, o := range bad {
		if validateOptions(map[string]interface{}{"assemblyai": o}) == nil {
			t.Fatalf("case %d (%v) accepted", i, o)
		}
	}
	good := map[string]interface{}{
		"audioEndAt": 1000, "contentSafetyConfidence": 25, "customSpelling": []interface{}{map[string]interface{}{"from": []interface{}{"a"}, "to": "b"}},
		"languageCode": "pt", "speechThreshold": 0.5, "temperature": nil, "removeAudioTags": "speaker",
		"redactStaticEntities": map[string]interface{}{"a": []interface{}{"b"}}, "wordBoost": []interface{}{"x"},
	}
	if err := validateOptions(map[string]interface{}{"assemblyai": good}); err != nil {
		t.Fatalf("valid options rejected: %v", err)
	}
}

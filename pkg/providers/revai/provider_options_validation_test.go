package revai

import "testing"

// TS parseProviderOptions(revaiTranscriptionModelOptionsSchema).
func TestProviderOptionsValidation(t *testing.T) {
	bad := []map[string]interface{}{
		{"metadata": 1},
		{"verbatim": nil},
		{"rush": "yes"},
		{"diarization_type": "basic"},
		{"notification_config": map[string]interface{}{}},
		{"notification_config": map[string]interface{}{"url": "u", "auth_headers": map[string]interface{}{}}},
		{"segments_to_transcribe": []interface{}{map[string]interface{}{"start": 1}}},
		{"speaker_names": []interface{}{map[string]interface{}{"display_name": 1}}},
		{"translation_config": map[string]interface{}{"target_languages": []interface{}{map[string]interface{}{"language": "xx"}}}},
		{"translation_config": map[string]interface{}{}},
		{"custom_vocabularies": []interface{}{"x"}},
		{"summarization_config": map[string]interface{}{"type": "list"}},
	}
	for _, o := range bad {
		if err := validateOptions(map[string]interface{}{"revai": o}); err == nil {
			t.Errorf("%v accepted", o)
		}
	}
	good := []map[string]interface{}{
		{},
		{"metadata": nil, "rush": nil, "language": "en", "verbatim": true, "diarization_type": "premium"},
		{"notification_config": map[string]interface{}{"url": "u", "auth_headers": map[string]interface{}{"Authorization": "t"}}},
		{"segments_to_transcribe": []interface{}{map[string]interface{}{"start": 1, "end": 2}}},
		{"translation_config": map[string]interface{}{"target_languages": []interface{}{map[string]interface{}{"language": "pt-br"}}, "model": "premium"}},
		{"custom_vocabularies": []interface{}{map[string]interface{}{}}},
	}
	for _, o := range good {
		if err := validateOptions(map[string]interface{}{"revai": o}); err != nil {
			t.Errorf("%v rejected: %v", o, err)
		}
	}
}

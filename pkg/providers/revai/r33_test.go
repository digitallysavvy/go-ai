package revai

import (
	"reflect"
	"testing"
)

// Nested z.object schemas strip unknown keys and keep explicit nulls;
// z.array(z.object({})) empties every vocabulary entry.
func TestR33_NestedZodStripping(t *testing.T) {
	cfg := revaiSeenConfig(t, map[string]interface{}{"revai": map[string]interface{}{
		"notification_config":    map[string]interface{}{"url": "u", "auth_headers": map[string]interface{}{"Authorization": "a", "x": 1}, "junk": 1},
		"segments_to_transcribe": []interface{}{map[string]interface{}{"start": 1.0, "end": 2.0, "x": 3}},
		"speaker_names":          []interface{}{map[string]interface{}{"display_name": "n", "x": 1}},
		"custom_vocabularies":    []interface{}{map[string]interface{}{"phrases": []interface{}{"a"}}},
		"summarization_config":   map[string]interface{}{"prompt": nil, "extra": 1},
		"translation_config":     map[string]interface{}{"target_languages": []interface{}{map[string]interface{}{"language": "fr", "x": 1}}, "extra": 1},
	}})
	want := map[string]interface{}{
		"notification_config":    map[string]interface{}{"url": "u", "auth_headers": map[string]interface{}{"Authorization": "a"}},
		"segments_to_transcribe": []interface{}{map[string]interface{}{"start": 1.0, "end": 2.0}},
		"speaker_names":          []interface{}{map[string]interface{}{"display_name": "n"}},
		"custom_vocabularies":    []interface{}{map[string]interface{}{}},
		"summarization_config":   map[string]interface{}{"prompt": nil, "model": "standard", "type": "paragraph"},
		"translation_config":     map[string]interface{}{"target_languages": []interface{}{map[string]interface{}{"language": "fr"}}, "model": "standard"},
	}
	for k, w := range want {
		if !reflect.DeepEqual(cfg[k], w) {
			t.Errorf("%s = %#v, want %#v", k, cfg[k], w)
		}
	}
}

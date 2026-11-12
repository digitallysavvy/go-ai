package telemetry

import "testing"

// GetBaseAttributes leaves out credential headers whatever their case.
func TestR92carryGetBaseAttributesSkipsCredentialHeadersCaseInsensitively(t *testing.T) {
	attrs := GetBaseAttributes("p", "m", nil, map[string]string{
		"authorization": "Bearer s1", "AUTHORIZATION": "Bearer s2", "X-Api-Key": "s3", "Api-Key": "s4", "x-trace": "t",
	})
	for _, kv := range attrs {
		switch string(kv.Key) {
		case "ai.model.provider", "ai.model.id", "ai.request.headers.x-trace":
		default:
			t.Fatalf("unexpected attribute %s = %s", kv.Key, kv.Value.String())
		}
	}
	if len(attrs) != 3 {
		t.Fatalf("attrs = %v", attrs)
	}
}

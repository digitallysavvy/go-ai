package claudecode

import (
	"encoding/json"
	"testing"
)

// TS validates lifecycle data with claudeCodeResumeStateSchema, so a
// malformed bridge coordinate or session id is rejected up front instead of
// being silently ignored on resume.
func TestValidateLifecycleStateDataRejectsMalformedData(t *testing.T) {
	h, err := New(Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.ValidateLifecycleStateData(json.RawMessage(`{"claudeSessionId":"s","bridge":{"port":1,"token":"t","lastSeenEventId":2}}`)); err != nil {
		t.Fatalf("valid data rejected: %v", err)
	}
	for _, bad := range []string{`{"claudeSessionId":5}`, `{"bridge":{"port":1,"token":"t"}}`, `[]`} {
		if err := h.ValidateLifecycleStateData(json.RawMessage(bad)); err == nil {
			t.Errorf("%s: expected a validation error", bad)
		}
	}
}

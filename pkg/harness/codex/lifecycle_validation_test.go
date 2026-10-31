package codex

import (
	"encoding/json"
	"testing"
)

func TestValidateLifecycleStateDataRejectsMalformedData(t *testing.T) {
	h := New(Settings{})
	if err := h.ValidateLifecycleStateData(json.RawMessage(`{"threadId":"s","bridge":{"port":1,"token":"t","lastSeenEventId":2}}`)); err != nil {
		t.Fatalf("valid data rejected: %v", err)
	}
	for _, bad := range []string{`{"threadId":5}`, `{"bridge":{"port":1,"token":"t"}}`, `[]`} {
		if err := h.ValidateLifecycleStateData(json.RawMessage(bad)); err == nil {
			t.Errorf("%s: expected a validation error", bad)
		}
	}
}

package zodlite

import "testing"

// JSON.parse reads 1e400 as Infinity, which z.number() rejects (and an
// undeclared key strips); encoding/json used to fail the frame, which
// ValidationMessage reported as "not JSON" (no issue).
func TestR96carryValidationMessageOutOfRange(t *testing.T) {
	schema := Object(Field{"n", Number})
	if msg := ValidationMessage(schema, `{"n":1e400}`); msg == "" {
		t.Error("an infinite number passed z.number()")
	}
	if msg := ValidationMessage(schema, `{"n":1,"extra":1e400}`); msg != "" {
		t.Errorf("an undeclared infinite key failed: %s", msg)
	}
}

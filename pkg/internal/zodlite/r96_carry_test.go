package zodlite

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
)

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

func decodeForTest(data string, v *interface{}) error {
	return jsonexact.Decode([]byte(data), v)
}

// cr-r96-provc-to-carry (provc's test).
func TestR96carryValidationMessageOutOfRangeNumber(t *testing.T) {
	node := Object(F("duration", Nullish(Number)))
	if msg := ValidationMessage(node, `{"duration":1e400}`); msg == "" {
		t.Fatal("declared z.number() holding 1e400 passed; JSON.parse reads Infinity, which z.number() rejects")
	}
	if msg := ValidationMessage(node, `{"duration":1,"x":1e400}`); msg != "" {
		t.Fatalf("undeclared key holding 1e400: %s", msg)
	}
}

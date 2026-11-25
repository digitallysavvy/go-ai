package gemini

import (
	"strings"
	"testing"
)

// provg1-04: the finalized streamed tool-call input is JSON.stringify of the
// accumulated object, so array-index keys come first (TS
// google-json-accumulator.ts:127; node at 9f7a842ae1 gives {"1":2,"b":"x"}).
func TestR91provgAccumulatorIntegerKeysFirst(t *testing.T) {
	acc := NewGoogleJSONAccumulator()
	acc.ProcessPartialArgs([]PartialArg{{JSONPath: "$.b", StringValue: strPtr("x")}})
	acc.ProcessPartialArgs([]PartialArg{{JSONPath: "$.1", NumberValue: numPtr(2)}})
	if got, _ := acc.Finalize(); got != `{"1":2,"b":"x"}` {
		t.Fatalf("final input = %s", got)
	}
}

// core3-04 (gemini part): deeply nested JSON is an error, not a fatal stack
// overflow.
func TestR91provgOrderedJSONDepthLimit(t *testing.T) {
	deep := strings.Repeat("[", 20000) + strings.Repeat("]", 20000)
	if _, err := decodeOrderedJSON([]byte(deep)); err == nil {
		t.Fatal("want a depth error")
	}
	ok := strings.Repeat("[", 100) + strings.Repeat("]", 100)
	if _, err := decodeOrderedJSON([]byte(ok)); err != nil {
		t.Fatalf("depth 100: %v", err)
	}
}

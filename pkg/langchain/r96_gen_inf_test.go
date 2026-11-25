package langchain

import (
	"math"
	"testing"
)

// Ruling 4 (round 96): LangGraph tool arguments beyond the double range parse
// as Infinity (secureJsonParse), and a GraphInterrupt message writes them as
// null (JSON.stringify(interrupts, null, 2)).
func TestR96genNonFiniteValues(t *testing.T) {
	args, _ := parseArgs(`{"a":1e400}`).(map[string]interface{})
	if v, _ := args["a"].(float64); !math.IsInf(v, 1) {
		t.Fatalf("args = %#v, want a = +Inf", args)
	}
	err := newGraphInterrupt(map[string]interface{}{"a": math.Inf(1)})
	if want := "{\n  \"a\": null\n}"; err.Error() != want {
		t.Fatalf("message = %q, want %q", err.Error(), want)
	}
}

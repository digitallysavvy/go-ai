package tsfixture

import "testing"

// vitest toHaveLength and `.length` read a JS string's UTF-16 length.
func TestR92carryStringLengthIsUTF16(t *testing.T) {
	a := Assertion{Expr: "result.text", Matcher: "toHaveLength", Arg: float64(3)}
	if msg, ok := MatchAssertion(a, "a😀"); !ok || msg != "" {
		t.Fatalf("toHaveLength(3) of a😀 = %q, %v", msg, ok)
	}
	if msg, _ := MatchAssertion(Assertion{Expr: "x", Matcher: "toHaveLength", Arg: float64(2)}, "a😀"); msg == "" {
		t.Fatal("toHaveLength(2) of a😀 passed")
	}
	if got, ok := evalPath(map[string]interface{}{"result": map[string]interface{}{"text": "😀"}}, nil, "result.text.length"); !ok || got != float64(2) {
		t.Fatalf("length = %v, %v", got, ok)
	}
}

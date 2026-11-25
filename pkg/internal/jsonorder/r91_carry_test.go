package jsonorder

import (
	"math"
	"strings"
	"testing"
)

// core3-03: a cyclic tool output must not crash the process (TS
// JSON.stringify throws a TypeError; Go returns the marshal-failure nil).
func TestR91carryToJSONValueCyclic(t *testing.T) {
	m := map[string]interface{}{}
	m["self"] = m
	if got := ToJSONValue(m); got != nil {
		t.Fatalf("got %v", got)
	}
	// Fan-out plus a NaN (the NaN retries run first without the cycle
	// check) must stay bounded too.
	n := map[string]interface{}{"a": math.NaN()}
	n["b"], n["c"] = n, n
	if got := ToJSONValue(n); got != nil {
		t.Fatalf("got %v", got)
	}
	s := []interface{}{nil}
	s[0] = s
	if got := ToJSONValue(s); got != nil {
		t.Fatalf("got %v", got)
	}
	type node struct {
		V    float64
		Next *node
	}
	p := &node{V: math.Inf(1)}
	p.Next = p
	if got := ToJSONValue(p); got != nil {
		t.Fatalf("got %v", got)
	}
}

// Shared but acyclic references still convert, and NaN still becomes null.
func TestR91carryToJSONValueSharedAcyclic(t *testing.T) {
	leaf := map[string]interface{}{"x": math.NaN()}
	v := map[string]interface{}{"a": leaf, "b": leaf}
	got, ok := ToJSONValue(v).(map[string]interface{})
	if !ok {
		t.Fatalf("got %#v", ToJSONValue(v))
	}
	a, _ := got["a"].(map[string]interface{})
	if a == nil || a["x"] != nil {
		t.Fatalf("got %#v", got)
	}
}

// core3-04: nesting past encoding/json's 10000 levels fails to parse
// instead of overflowing the stack.
func TestR91carryParseDepthLimit(t *testing.T) {
	const n = 2_000_000
	deep := strings.Repeat("[", n) + strings.Repeat("]", n)
	if _, ok := Parse([]byte(deep)); ok {
		t.Fatal("deep array parsed")
	}
	deepObj := strings.Repeat(`{"a":`, n) + "1" + strings.Repeat("}", n)
	if _, ok := Parse([]byte(deepObj)); ok {
		t.Fatal("deep object parsed")
	}
	ok10k := strings.Repeat("[", maxParseDepth) + strings.Repeat("]", maxParseDepth)
	if _, ok := Parse([]byte(ok10k)); !ok {
		t.Fatal("10000 levels must parse (encoding/json accepts them)")
	}
	over := strings.Repeat("[", maxParseDepth+1) + strings.Repeat("]", maxParseDepth+1)
	if _, ok := Parse([]byte(over)); ok {
		t.Fatal("10001 levels must fail")
	}
}

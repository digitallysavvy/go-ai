package ai

import (
	"context"
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// Ruling 4 (round 97): Output.object / Output.array return the parsed value,
// so a number beyond the double range is Infinity (TS JSON.parse), not a
// failed conversion.
func TestR97genOutputNonFiniteNumber(t *testing.T) {
	ctx := context.Background()
	anyObject := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})

	obj := ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: anyObject})
	got, err := obj.ParseCompleteOutput(ctx, ParseCompleteOutputOptions{Text: `{"a":1e400}`})
	if err != nil {
		t.Fatalf("object complete: %v", err)
	}
	if v, _ := got["a"].(float64); !math.IsInf(v, 1) {
		t.Fatalf("object complete a = %#v, want +Inf", got["a"])
	}
	partial, err := obj.ParsePartialOutput(ctx, ParsePartialOutputOptions{Text: `{"a":-1e400,"b":`})
	if err != nil || partial == nil {
		t.Fatalf("object partial = %#v, %v; want a partial", partial, err)
	}
	if v, _ := partial.Partial["a"].(float64); !math.IsInf(v, -1) {
		t.Fatalf("object partial a = %#v, want -Inf", partial.Partial["a"])
	}

	arr := ArrayOutput[map[string]interface{}](ArrayOutputOptions[map[string]interface{}]{ElementSchema: anyObject})
	elems, err := arr.ParseCompleteOutput(ctx, ParseCompleteOutputOptions{Text: `{"elements":[{"a":1e400}]}`})
	if err != nil || len(elems) != 1 {
		t.Fatalf("array complete = %#v, %v", elems, err)
	}
	if v, _ := elems[0]["a"].(float64); !math.IsInf(v, 1) {
		t.Fatalf("array complete a = %#v, want +Inf", elems[0]["a"])
	}
	parr, err := arr.ParsePartialOutput(ctx, ParsePartialOutputOptions{Text: `{"elements":[{"a":1e400},{"b":`})
	if err != nil || parr == nil || len(parr.Partial) != 1 {
		t.Fatalf("array partial = %#v, %v; want one element", parr, err)
	}
	if v, _ := parr.Partial[0]["a"].(float64); !math.IsInf(v, 1) {
		t.Fatalf("array partial a = %#v, want +Inf", parr.Partial[0]["a"])
	}

	// A typed target cannot hold Infinity: the value converts through its
	// JSON.stringify text (null) instead of failing.
	type r97genTyped struct {
		A *float64 `json:"a"`
		B string   `json:"b"`
	}
	typed := ObjectOutput[r97genTyped](ObjectOutputOptions{Schema: anyObject})
	tv, err := typed.ParseCompleteOutput(ctx, ParseCompleteOutputOptions{Text: `{"a":1e400,"b":"x"}`})
	if err != nil || tv.A != nil || tv.B != "x" {
		t.Fatalf("typed complete = %#v, %v; want a nil, b x", tv, err)
	}
}

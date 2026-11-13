package securejson

import (
	"errors"
	"math"
	"testing"
)

func TestR96carryUnmarshalOutOfRange(t *testing.T) {
	var got map[string]interface{}
	if err := Unmarshal([]byte(`{"a":{"b":-1e400}}`), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if f, _ := got["a"].(map[string]interface{})["b"].(float64); !math.IsInf(f, -1) {
		t.Errorf("got %#v", got)
	}
	if err := Unmarshal([]byte(`{"x":1e400,"__proto__":{}}`), &got); !errors.Is(err, ErrForbiddenPrototype) {
		t.Errorf("forbidden key err = %v", err)
	}
}

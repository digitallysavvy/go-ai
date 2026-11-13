package jsonexact

import (
	"errors"
	"testing"
)

// TS secureJsonParse rejects __proto__ / constructor.prototype at any depth
// (also behind \u escapes) with a JSONParseError; other payloads decode.
func TestR96carryUnmarshalAndDecodeRejectForbiddenPrototype(t *testing.T) {
	bad := []string{
		`{"__proto__":{"x":1}}`,
		`{"a":[{"b":{"__proto__":null}}]}`,
		`{"constructor":{"prototype":{}}}`,
		`{"__proto__":1}`,
	}
	for _, s := range bad {
		for name, fn := range map[string]func([]byte, interface{}) error{"Unmarshal": Unmarshal, "Decode": Decode} {
			var v map[string]interface{}
			err := fn([]byte(s), &v)
			var pe *JSONParseError
			if !errors.As(err, &pe) || !errors.Is(err, ErrForbiddenPrototype) {
				t.Fatalf("%s(%s) = %v, want a JSONParseError", name, s, err)
			}
			want := "JSON parsing failed: Text: " + s + ".\nError message: Object contains forbidden prototype property"
			if err.Error() != want {
				t.Errorf("%s(%s) text = %q, want %q", name, s, err.Error(), want)
			}
		}
	}
	for _, s := range []string{`{"constructor":"x"}`, `{"constructor":{"name":"y"}}`, `{"a":"__proto__"}`, `{"proto":1}`} {
		var v struct {
			A interface{} `json:"a"`
		}
		if err := Unmarshal([]byte(s), &v); err != nil {
			t.Errorf("Unmarshal(%s): %v", s, err)
		}
	}
}

package securejson

import "testing"

func TestRejectsForbiddenPrototypeKeys(t *testing.T) {
	bad := []string{
		`{"__proto__":{"x":1}}`,
		`{"a":[{"b":{"__proto__":null}}]}`,
		`{"__proto__":1}`,
		`{"constructor":{"prototype":{}}}`,
		`[{"constructor":{"prototype":1}}]`,
	}
	for _, s := range bad {
		var v any
		if err := Unmarshal([]byte(s), &v); err != ErrForbiddenPrototype {
			t.Errorf("%s: err=%v", s, err)
		}
	}
	good := []string{`{"constructor":"x"}`, `{"constructor":{"name":"y"}}`, `{"a":"__proto__"}`, `{"proto":1}`, `{"é":"é"}`}
	for _, s := range good {
		var v any
		if err := Unmarshal([]byte(s), &v); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	var v any
	if err := Unmarshal([]byte(`{`), &v); err == nil || err == ErrForbiddenPrototype {
		t.Errorf("invalid JSON must report a syntax error, got %v", err)
	}
}

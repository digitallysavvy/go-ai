package securejson

import (
	"errors"
	"testing"
)

// secure-json-parse rejects a forbidden key even when another number is out
// of the double range (JSON.parse reads it as Infinity).
func TestR94carryForbiddenKeyNextToHugeNumber(t *testing.T) {
	for _, body := range []string{
		`{"x":1e400,"__proto__":{}}`,
		`[1e400,{"constructor":{"prototype":{}}}]`,
	} {
		if err := Check([]byte(body)); !errors.Is(err, ErrForbiddenPrototype) {
			t.Errorf("Check(%s) = %v, want ErrForbiddenPrototype", body, err)
		}
	}
	if err := Check([]byte(`{"x":1e400,"constructor":1}`)); err != nil {
		t.Errorf("Check = %v, want nil", err)
	}
}

// Unmarshal matches keys exactly (zod after secureJsonParse).
func TestR94carryUnmarshalMatchesKeysExactly(t *testing.T) {
	var v struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := Unmarshal([]byte(`{"error":{"Message":"m"}}`), &v); err != nil || v.Error == nil || v.Error.Message != "" {
		t.Errorf("Unmarshal = %+v, %v", v.Error, err)
	}
}

package harness

import (
	"errors"
	"testing"
)

func TestErrorValueMessageMatchesTSGetErrorMessage(t *testing.T) {
	if got := ErrorValueMessage(nil); got != "unknown error" {
		t.Fatalf("nil = %q", got)
	}
	if got := ErrorValueMessage("boom"); got != "boom" {
		t.Fatalf("string = %q", got)
	}
	if got := ErrorValueMessage(errors.New("bad")); got != "bad" {
		t.Fatalf("error = %q", got)
	}
	if got := ErrorValueMessage(map[string]any{"message": "a<b", "name": "X"}); got != `{"message":"a<b","name":"X"}` {
		t.Fatalf("object = %q", got)
	}
	sentinel := errors.New("typed")
	if ErrorFromValue(sentinel) != sentinel {
		t.Fatal("an error value must pass through unchanged")
	}
	if got := ErrorFromValue(map[string]any{"code": 3}).Error(); got != `{"code":3}` {
		t.Fatalf("wrapped object = %q", got)
	}
}

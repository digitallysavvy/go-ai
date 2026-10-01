package codemode

import (
	"testing"
	"time"
)

// Ports TypeScript's code-mode/src/utils/serialization.ts behavior
// (exercised indirectly by exceptions.test.ts's host-tool-output cases;
// tested directly here since it is pure and Go-specific in its handling
// of nil vs Date).
func TestToJSONPayload_NilReturnsEmptyString(t *testing.T) {
	got, err := toJSONPayload(nil, 1024, "value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestToJSONPayload_TimeFormatsAsJSISOString(t *testing.T) {
	got, err := toJSONPayload(time.Unix(0, 0).UTC(), 1024, "value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != `"1970-01-01T00:00:00.000Z"` {
		t.Fatalf("got %q", got)
	}
}

func TestToJSONPayload_OmitsNilMapEntries(t *testing.T) {
	got, err := toJSONPayload(map[string]interface{}{"a": "x", "b": nil}, 1024, "value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != `{"a":"x"}` {
		t.Fatalf("got %q", got)
	}
}

func TestToJSONPayload_SizeLimit(t *testing.T) {
	_, err := toJSONPayload("abcdef", 4, "value")
	if err == nil {
		t.Fatal("expected a size-limit error")
	}
	var cmErr CodeModeError
	if e, ok := err.(CodeModeError); ok {
		cmErr = e
	} else {
		t.Fatalf("expected a CodeModeError, got %T", err)
	}
	if cmErr.ErrorCode() != "CODE_MODE_SERIALIZATION_ERROR" {
		t.Fatalf("got code %q", cmErr.ErrorCode())
	}
}

func TestToJSONPayload_CircularReference(t *testing.T) {
	m := map[string]interface{}{"value": "x"}
	m["self"] = m
	_, err := toJSONPayload(m, 1024*1024, "value")
	if err == nil {
		t.Fatal("expected a circular-reference error")
	}
}

func TestFromJSONPayload_RoundTrip(t *testing.T) {
	got, err := fromJSONPayload(`{"a":1,"b":[1,2,3]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("got %T", got)
	}
	if m["a"] != float64(1) {
		t.Fatalf("got %#v", m["a"])
	}
}

func TestFromJSONPayload_EmptyStringIsNil(t *testing.T) {
	got, err := fromJSONPayload("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("got %#v", got)
	}
}

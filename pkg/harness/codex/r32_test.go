package codex

import (
	"bytes"
	"testing"
)

func TestParseCodexAuthFileRejectsForbiddenPrototypeKey(t *testing.T) {
	if parseCodexAuthFile(`{"a":1}`) == nil {
		t.Fatal("control must parse")
	}
	if parseCodexAuthFile(`{"a":1,"constructor":{"prototype":{}}}`) != nil {
		t.Fatal("constructor.prototype must be rejected")
	}
}

// The fingerprint is JSON.stringify over Object.fromEntries(sorted entries); a
// JS object enumerates integer-like keys first in ascending numeric order, so
// "9" precedes "10" and both precede "a".
func TestCanonicalJSONHoistsIntegerLikeKeys(t *testing.T) {
	var buf bytes.Buffer
	writeJSCanonicalJSON(&buf, map[string]any{"a": true, "10": false, "9": nil, "b": []any{}})
	if got, want := buf.String(), `{"9":null,"10":false,"a":true,"b":[]}`; got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

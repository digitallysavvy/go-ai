package codex

import "testing"

func TestParseCodexAuthFileRejectsForbiddenPrototypeKey(t *testing.T) {
	if parseCodexAuthFile(`{"a":1}`) == nil {
		t.Fatal("control must parse")
	}
	if parseCodexAuthFile(`{"a":1,"constructor":{"prototype":{}}}`) != nil {
		t.Fatal("constructor.prototype must be rejected")
	}
}

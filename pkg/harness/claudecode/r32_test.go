package claudecode

import "testing"

func TestParseClaudeCredentialFileRejectsForbiddenPrototypeKey(t *testing.T) {
	if parseClaudeCredentialFile(`{"a":1}`) == nil {
		t.Fatal("control must parse")
	}
	if parseClaudeCredentialFile(`{"__proto__":{}}`) != nil {
		t.Fatal("__proto__ must be rejected")
	}
}

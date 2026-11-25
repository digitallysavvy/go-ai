package vercel

import (
	"context"
	"testing"
	"unicode/utf8"
)

// TS listDirectory takes record.slice(0, -1) as the type of a record without
// a space: the last UTF-16 code unit is dropped, not the last byte (Go left
// a broken UTF-8 sequence behind for a non-ASCII last character).
func TestR96toolsListDirectoryRecordWithoutSpace(t *testing.T) {
	f := newFakeServer()
	defer f.Close()
	f.CommandFn = func(string, string, []string, string, map[string]string) CommandScript {
		return CommandScript{Stdout: "fileé\x00ab\x00"}
	}
	sess := NewSession(newTestSandbox(t, f, "sbx_r96"))
	entries, err := sess.ListDirectory(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Type != "file" || entries[0].Name != "fileé" || entries[1].Type != "a" || entries[1].Name != "ab" {
		t.Fatalf("entries = %#v", entries)
	}
	if !utf8.ValidString(entries[0].Type) {
		t.Fatalf("type %q is not valid UTF-8", entries[0].Type)
	}
}

package codemode

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"testing"
)

// TestStripTypeScriptAnnotationsMatchesNode replays snippets through Node's
// stripTypeScriptTypes (v24, the strip mode the TS code-mode run package uses,
// with the snippet wrapped in `async function __runUser__(){ ... }` as
// stripSnippetTypes does) and compares the output byte for byte: erased syntax
// is blanked with spaces, line breaks kept, and a snippet Node rejects comes
// back unmodified. The cases cover what model-written snippets contain:
// annotations, function types, ASI without semicolons, class members, object
// methods, template interpolations, generics and `!`.
//
// The cases are the base snippets plus variants (no semicolons, CRLF, tab
// indentation, wrapped in a block, doubled spaces). Known leniencies are left
// out: a line break before `as` (`a as T` newline `as U`), which Node rejects
// and this stripper reads anyway, and Node's habit of moving a `;` to the start
// of an erased type alias when `;` newline `(` follows it.
func TestStripTypeScriptAnnotationsMatchesNode(t *testing.T) {
	f, err := os.Open("testdata/strip_node_cases.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Src string `json:"src"`
		Out string `json:"out"`
		OK  bool   `json:"ok"`
	}
	if err := json.NewDecoder(zr).Decode(&cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 1500 {
		t.Fatalf("fixture has %d cases", len(cases))
	}
	for _, c := range cases {
		got, err := stripTypeScriptAnnotations(c.Src)
		if got != c.Out {
			t.Errorf("src %q (node ok=%v, go err=%v)\n got  %q\n want %q", c.Src, c.OK, err, got, c.Out)
		}
	}
}

func TestStripTypeScriptAnnotationsKeepsPrefixNot(t *testing.T) {
	for _, src := range []string{"const a = !!b; return a;", "if (!!x) return 1", "return !!(a && b);", "foo()\n!bar();", "if (ok) !done && run();"} {
		got, err := stripTypeScriptAnnotations(src)
		if err != nil || got != src {
			t.Errorf("%q -> %q, %v", src, got, err)
		}
	}
	got, _ := stripTypeScriptAnnotations("const a = x!!;")
	if got != "const a = x  ;" {
		t.Errorf("non-null chain = %q", got)
	}
}

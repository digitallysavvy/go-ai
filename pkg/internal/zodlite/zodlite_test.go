package zodlite

import "testing"

func TestValidationMessage(t *testing.T) {
	schema := Disc(map[string]Node{
		"a": Typed("a", F("n", Number), F("s", Nullish(String)), F("l", Optional(Array(Boolean)))),
	})
	if got := ValidationMessage(schema, `{"type":"a","n":1}`); got != "" {
		t.Fatalf("valid frame rejected: %q", got)
	}
	if got := ValidationMessage(schema, `not json`); got != "" {
		t.Fatalf("non-JSON must be reported elsewhere: %q", got)
	}
	want := "Type validation failed: Value: {\"type\":\"a\",\"n\":\"x\"}.\nError message: [{\"code\":\"invalid_type\",\"message\":\"Invalid input: expected number, received string\",\"path\":[\"n\"]}]"
	if got := ValidationMessage(schema, `{ "type": "a", "n": "x" }`); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if got := ValidationMessage(schema, `{"type":"b"}`); got == "" {
		t.Fatal("unknown discriminator accepted")
	}
	if got := IssueText("x.\nError message: [i]"); got != "[i]" {
		t.Fatal(got)
	}
	if got := CompactJSON("{ bad"); got != "{ bad" {
		t.Fatal(got)
	}
	if Nullable(String)(nil, false, nil) == nil || Nullable(String)(nil, true, nil) != nil {
		t.Fatal("nullable semantics")
	}
}

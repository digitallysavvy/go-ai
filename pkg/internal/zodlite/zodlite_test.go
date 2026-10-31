package zodlite

import (
	"encoding/json"
	"strings"
	"testing"
)

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
	want := "Type validation failed: Value: {\"type\":\"a\",\"n\":\"x\"}.\nError message: [\n  {\n    \"expected\": \"number\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"n\"\n    ],\n    \"message\": \"Invalid input: expected number, received string\"\n  }\n]"
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

func TestEnumRecordOfAndIssueError(t *testing.T) {
	e := Enum("a", "b")
	if e("a", true, nil) != nil {
		t.Fatal("a should pass")
	}
	bad := e("c", true, []interface{}{"k"})
	if bad == nil || bad.Message != `Invalid option: expected one of "a"|"b"` || bad.Code != "invalid_value" {
		t.Fatalf("enum issue = %+v", bad)
	}
	if one := Enum("click")("x", true, nil); one == nil || one.Message != `Invalid input: expected "click"` {
		t.Fatalf("single-value enum must use the zod v4 literal message, got %+v", one)
	}
	rec := RecordOf(String)
	if rec(map[string]interface{}{"x": "y"}, true, nil) != nil {
		t.Fatal("string record should pass")
	}
	got := rec(map[string]interface{}{"x": 1.0}, true, []interface{}{"r"})
	if got == nil || got.Error() != "r.x: Invalid input: expected string, received number" {
		t.Fatalf("record issue = %v", got)
	}
	if rec("s", true, nil).Message != "Invalid input: expected record, received string" {
		t.Fatal("record type message")
	}
	idx := Array(String)([]interface{}{"a", 2.0}, true, []interface{}{"items"})
	if idx.Error() != "items[1]: Invalid input: expected string, received number" {
		t.Fatalf("index path = %q", idx.Error())
	}
	var empty error = &Issue{Message: "m"}
	if empty.Error() != "m" {
		t.Fatal("empty path renders the message alone")
	}
}

func TestIssueJSONShapes(t *testing.T) {
	lit := ValidationMessage(Typed("a"), `{"type":"b"}`)
	if want := "Error message: [\n  {\n    \"code\": \"invalid_value\",\n    \"values\": [\n      \"a\"\n    ],\n    \"path\": [\n      \"type\"\n    ],\n    \"message\": \"Invalid input: expected \\\"a\\\"\"\n  }\n]"; IssueText(lit) != want[len("Error message: "):] {
		t.Fatalf("literal issue = %q", IssueText(lit))
	}
	if got := IssueText(ValidationMessage(Disc(map[string]Node{"a": Typed("a")}), `{"type":"z"}`)); !strings.Contains(got, `"note": "No matching discriminator"`) || !strings.Contains(got, `"discriminator": "type"`) {
		t.Fatalf("disc issue = %q", got)
	}
}

func TestUnionNestedErrors(t *testing.T) {
	u := Union(String, Number)
	got := IssueText(ValidationMessage(Object(F("a", u)), `{"a":true}`))
	want := "[\n  {\n    \"code\": \"invalid_union\",\n    \"errors\": [\n      [\n        {\n          \"expected\": \"string\",\n          \"code\": \"invalid_type\",\n          \"path\": [],\n          \"message\": \"Invalid input: expected string, received boolean\"\n        }\n      ],\n      [\n        {\n          \"expected\": \"number\",\n          \"code\": \"invalid_type\",\n          \"path\": [],\n          \"message\": \"Invalid input: expected number, received boolean\"\n        }\n      ]\n    ],\n    \"path\": [\n      \"a\"\n    ],\n    \"message\": \"Invalid input\"\n  }\n]"
	if got != want {
		t.Fatalf("got %s", got)
	}
}

func TestValidationMessageValueIsJSStringify(t *testing.T) {
	schema := Object(F("n", String))
	msg := ValidationMessage(schema, `{"b":1.0,"a":"é<","n":2e2}`)
	want := `Value: {"b":1,"a":"é<","n":200}.`
	if !strings.Contains(msg, want) {
		t.Fatalf("message %q lacks %q", msg, want)
	}
}

func TestAllIssuesReported(t *testing.T) {
	schema := Object(F("a", String), F("b", Number), F("c", Array(String)), F("r", RecordOf(Number)))
	got := IssueText(ValidationMessage(schema, `{"a":1,"c":["x",2,3],"r":{"z":"s","y":"t"}}`))
	var issues []struct {
		Path    []interface{} `json:"path"`
		Message string        `json:"message"`
	}
	if err := json.Unmarshal([]byte(got), &issues); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, i := range issues {
		b, _ := json.Marshal(i.Path)
		paths = append(paths, string(b))
	}
	want := `["a"] ["b"] ["c",1] ["c",2] ["r","y"] ["r","z"]`
	if strings.Join(paths, " ") != want {
		t.Fatalf("paths = %s, want %s", strings.Join(paths, " "), want)
	}
}

func TestUnionMemberReportsAllIssues(t *testing.T) {
	u := Union(Object(F("a", String), F("b", String)))
	got := IssueText(ValidationMessage(u, `{}`))
	if strings.Count(got, `"code": "invalid_type"`) != 2 {
		t.Fatalf("want both member issues, got %s", got)
	}
}

func TestLiteralMessageIsRawQuoted(t *testing.T) {
	got := Literal(`a"<b`)("x", true, nil).Message
	if got != `Invalid input: expected "a"<b"` {
		t.Fatalf("message = %q", got)
	}
}

// z.object({}).strict(): zod v4 unrecognized_keys, "key" vs "keys".
func TestStrictEmptyObject(t *testing.T) {
	if StrictEmptyObject(map[string]interface{}{}, true, nil) != nil {
		t.Fatal("empty object rejected")
	}
	one := StrictEmptyObject(map[string]interface{}{"a": 1.0}, true, nil)
	if one == nil || one.Message != `Unrecognized key: "a"` || string(one.json()) != `{"code":"unrecognized_keys","keys":["a"],"path":[],"message":"Unrecognized key: \"a\""}` {
		t.Fatalf("one key: %+v %s", one, one.json())
	}
	two := StrictEmptyObject(map[string]interface{}{"b": 1.0, "a": 1.0}, true, nil)
	if two == nil || two.Message != `Unrecognized keys: "a", "b"` {
		t.Fatalf("two keys: %+v", two)
	}
	if bad := StrictEmptyObject("s", true, nil); bad == nil || bad.Code != "invalid_type" {
		t.Fatalf("non-object: %+v", bad)
	}
}

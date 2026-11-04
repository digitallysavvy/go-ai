package workflow

import (
	"encoding/json"
	"reflect"
	"regexp"
	"testing"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS parseInt(response.headers.get('x-workflow-stream-tail-index'), 10):
// Headers.get decodes the header bytes as Latin-1, so a raw 0xA0 byte is
// U+00A0 and parseInt skips it; a UTF-8 encoded U+00A0 is "Â " (NaN).
// Recorded with Node 24 fetch against a raw TCP server: a0 35 -> 5,
// a0 a0 2d 33 -> -3, c2 a0 38 -> NaN.
func TestR86bcarryTailIndexLatin1Whitespace(t *testing.T) {
	for _, c := range []struct {
		header string
		want   string
	}{
		{"\xa0" + "5", "?startIndex=1"},
		{"\xa0\xa0-3", "?startIndex=0"},
		{"\xa0" + "40", "?startIndex=36"},
		{"\xc2\xa0" + "8", "?startIndex=0"},
	} {
		srv := newScriptedServer(t, scriptedResponse{header: map[string]string{"x-workflow-stream-tail-index": c.header}}, scriptedResponse{frames: []string{`{"type":"finish"}`}})
		_ = captureLog(t)
		tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.srv.URL + "/api/chat", InitialStartIndex: -5})
		reconnect(t, tr, ai.ChatTransportReconnectToStreamRequest{ChatID: "c"})
		want := []string{"/api/chat/c/stream?startIndex=-5", "/api/chat/c/stream" + c.want}
		if got := srv.requested(); !reflect.DeepEqual(got, want) {
			t.Errorf("tail %q: requests = %v, want %v", c.header, got, want)
		}
		if n, ok := parseIntPrefix(c.header); c.header == "\xc2\xa08" && ok {
			t.Errorf("parseIntPrefix(%q) = %d, true; want NaN", c.header, n)
		}
	}
}

// r86bcarryAjvPatternCases were recorded with Ajv 8.20.0 (TS
// resolveSerializableTools options: allErrors, strict false, validateFormats
// false) at 30548ee7f9; error is the formatAjvError join, "" when valid.
const r86bcarryAjvPatternCases = `[
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^\\s+$"}}}, "input": {"v": "\u00a0"}, "error": ""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^\\s+$"}}}, "input": {"v": "\u000b"}, "error": ""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^\\s+$"}}}, "input": {"v": "\u3000"}, "error": ""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^\\s+$"}}}, "input": {"v": "\ufeff"}, "error": ""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^\\s+$"}}}, "input": {"v": "\u0085"}, "error": "data/v must match pattern \"^\\s+$\""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^\\s+$"}}}, "input": {"v": " "}, "error": ""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^\\S+$"}}}, "input": {"v": "a\u00a0b"}, "error": "data/v must match pattern \"^\\S+$\""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^\\S+$"}}}, "input": {"v": "\u0085"}, "error": ""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^\\S+$"}}}, "input": {"v": "ab"}, "error": ""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^[\\s\\d]+$"}}}, "input": {"v": "1\u20002"}, "error": ""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^[\\s\\d]+$"}}}, "input": {"v": "1x"}, "error": "data/v must match pattern \"^[\\s\\d]+$\""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^[^\\S]+$"}}}, "input": {"v": "\u2028"}, "error": ""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^[^\\S]+$"}}}, "input": {"v": "a"}, "error": "data/v must match pattern \"^[^\\S]+$\""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^a.b$"}}}, "input": {"v": "a\rb"}, "error": "data/v must match pattern \"^a.b$\""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^a.b$"}}}, "input": {"v": "a\u2028b"}, "error": "data/v must match pattern \"^a.b$\""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^a.b$"}}}, "input": {"v": "a\u0085b"}, "error": ""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^a.b$"}}}, "input": {"v": "a\nb"}, "error": "data/v must match pattern \"^a.b$\""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^a.b$"}}}, "input": {"v": "axb"}, "error": ""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^\\u0041\\u{1F600}$"}}}, "input": {"v": "A\ud83d\ude00"}, "error": ""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^\\u0041\\u{1F600}$"}}}, "input": {"v": "A"}, "error": "data/v must match pattern \"^\\u0041\\u{1F600}$\""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^\\uD83D\\uDE00$"}}}, "input": {"v": "\ud83d\ude00"}, "error": ""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^\\uD83D\\uDE00$"}}}, "input": {"v": "x"}, "error": "data/v must match pattern \"^\\uD83D\\uDE00$\""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "^[^]$"}}}, "input": {"v": "\n"}, "error": ""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "[]"}}}, "input": {"v": ""}, "error": "data/v must match pattern \"[]\""},
	{"schema": {"type": "object", "properties": {"v": {"type": "string", "pattern": "[]"}}}, "input": {"v": "a"}, "error": "data/v must match pattern \"[]\""},
	{"schema": {"type": "object", "additionalProperties": false, "patternProperties": {"^\\s": {}}}, "input": {"\u00a0x": 1}, "error": ""},
	{"schema": {"type": "object", "additionalProperties": false, "patternProperties": {"^\\s": {}}}, "input": {"\u000bx": 1}, "error": ""},
	{"schema": {"type": "object", "additionalProperties": false, "patternProperties": {"^\\s": {}}}, "input": {"x": 1}, "error": "data has unexpected property \"x\""}
]`

// TS validates serializable tool input with Ajv, whose patterns are
// JavaScript u-mode RegExps: `\s` / `\S` cover the Unicode spaces and \v,
// `.` excludes \r and U+2028/U+2029, `\uXXXX` / `\u{...}` are code points,
// and `[]` / `[^]` match nothing / anything.
func TestR86bcarryAjvPatternSemantics(t *testing.T) {
	var cases []struct {
		Schema map[string]interface{} `json:"schema"`
		Input  interface{}            `json:"input"`
		Error  string                 `json:"error"`
	}
	if err := json.Unmarshal([]byte(r86bcarryAjvPatternCases), &cases); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		_, msg, err := validateSerializableInput(SerializableToolDef{Name: "t", InputSchema: c.Schema}, c.Input)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if msg != c.Error {
			t.Errorf("case %d (%v, input %q): error %q, want %q", i, c.Schema, c.Input, msg, c.Error)
		}
	}
}

// The RE2 classes for JS `\s` / `\S` / `.` agree with providerutils.IsJSWhiteSpace
// on every code point.
func TestR86bcarryAjvPatternClassesMatchJSWhiteSpace(t *testing.T) {
	space := regexp.MustCompile(ajvPatternToRE2(`^\s$`))
	nonSpace := regexp.MustCompile(ajvPatternToRE2(`^\S$`))
	inClass := regexp.MustCompile(ajvPatternToRE2(`^[^\S]$`))
	dot := regexp.MustCompile(ajvPatternToRE2(`^.$`))
	for r := rune(0); r <= utf8.MaxRune; r++ {
		if r >= 0xd800 && r <= 0xdfff {
			continue
		}
		s := string(r)
		ws := providerutils.IsJSWhiteSpace(r)
		if space.MatchString(s) != ws || nonSpace.MatchString(s) == ws || inClass.MatchString(s) != ws {
			t.Fatalf("U+%04X: \\s=%v \\S=%v [^\\S]=%v, JS whitespace %v", r, space.MatchString(s), nonSpace.MatchString(s), inClass.MatchString(s), ws)
		}
		lt := r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029
		if dot.MatchString(s) == lt {
			t.Fatalf("U+%04X: . = %v", r, dot.MatchString(s))
		}
	}
}

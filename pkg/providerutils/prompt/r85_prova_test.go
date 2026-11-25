package prompt

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// r85provaToolInputCases are TS outputs (ai@30548ee7f9, TS 4ae6986cd8) for
// a tool call whose input is the TS string s, recorded by running
// secureJsonParse and the converters' serializeToolCallArguments /
// toAnthropicToolInput / parseToolCallInput logic under Node.
var r85provaToolInputCases = []struct{ s, serialize, anthropic, google string }{
	{"{\"a\":1}", "{\"a\":1}", "{\"a\":1}", "{\"a\":1}"},
	{"[1,2]", "[1,2]", "{\"rawInvalidInput\":\"[1,2]\"}", "[1,2]"},
	{"\"x\"", "\"x\"", "{\"rawInvalidInput\":\"\\\"x\\\"\"}", "\"x\""},
	{"x", "\"x\"", "{\"rawInvalidInput\":\"x\"}", "\"x\""},
	{"{\"__proto__\":{\"b\":1}}", "\"{\\\"__proto__\\\":{\\\"b\\\":1}}\"", "{\"rawInvalidInput\":\"{\\\"__proto__\\\":{\\\"b\\\":1}}\"}", "\"{\\\"__proto__\\\":{\\\"b\\\":1}}\""},
	{"{\"constructor\":{\"prototype\":{}}}", "\"{\\\"constructor\\\":{\\\"prototype\\\":{}}}\"", "{\"rawInvalidInput\":\"{\\\"constructor\\\":{\\\"prototype\\\":{}}}\"}", "\"{\\\"constructor\\\":{\\\"prototype\\\":{}}}\""},
	{"{\"constructor\":{\"a\":1}}", "{\"constructor\":{\"a\":1}}", "{\"constructor\":{\"a\":1}}", "{\"constructor\":{\"a\":1}}"},
	{"  {\"a\":1} ", "  {\"a\":1} ", "{\"a\":1}", "{\"a\":1}"},
	{"1e5", "1e5", "{\"rawInvalidInput\":\"1e5\"}", "100000"},
	{"null", "null", "{\"rawInvalidInput\":\"null\"}", "null"},
	{"{\"a\":", "\"{\\\"a\\\":\"", "{\"rawInvalidInput\":\"{\\\"a\\\":\"}", "\"{\\\"a\\\":\""},
	{"{\"b\":2,\"a\":1}", "{\"b\":2,\"a\":1}", "{\"b\":2,\"a\":1}", "{\"b\":2,\"a\":1}"},
	{" ", "\" \"", "{\"rawInvalidInput\":\" \"}", "\" \""},
	{"<tag>&", "\"<tag>&\"", "{\"rawInvalidInput\":\"<tag>&\"}", "\"<tag>&\""},
	{"{\"x\":\"\\u005f\"}", "{\"x\":\"\\u005f\"}", "{\"x\":\"_\"}", "{\"x\":\"_\"}"},
	{"{\"nested\":{\"__proto__\":1}}", "\"{\\\"nested\\\":{\\\"__proto__\\\":1}}\"", "{\"rawInvalidInput\":\"{\\\"nested\\\":{\\\"__proto__\\\":1}}\"}", "\"{\\\"nested\\\":{\\\"__proto__\\\":1}}\""},
	{"[{\"__proto__\":1}]", "\"[{\\\"__proto__\\\":1}]\"", "{\"rawInvalidInput\":\"[{\\\"__proto__\\\":1}]\"}", "\"[{\\\"__proto__\\\":1}]\""},
	{"true", "true", "{\"rawInvalidInput\":\"true\"}", "true"},
	{"", "\"\"", "{\"rawInvalidInput\":\"\"}", "\"\""},
}

func r85provaSameJSON(t *testing.T, label, got, want string) {
	t.Helper()
	var g, w interface{}
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Errorf("%s: got %q is not JSON", label, got)
		return
	}
	_ = json.Unmarshal([]byte(want), &w)
	if !reflect.DeepEqual(g, w) {
		t.Errorf("%s: got %s, TS %s", label, got, want)
	}
}

func TestR85provaToolInputSerializationDifferential(t *testing.T) {
	for _, c := range r85provaToolInputCases {
		// Go keeps a TS string input as a JSON string literal (MarshalJSON
		// of ToolCallContent writes it back as that string).
		literal, _ := jsonstr.Marshal(c.s)
		if got := SerializeToolCallArguments(string(literal)); got != c.serialize {
			t.Errorf("SerializeToolCallArguments(%s) = %q, TS %q", literal, got, c.serialize)
		}
		if !json.Valid([]byte(c.s)) && c.s != "" {
			// A Go input that is not JSON is kept as its raw text.
			if got := SerializeToolCallArguments(c.s); got != c.serialize {
				t.Errorf("SerializeToolCallArguments(raw %q) = %q, TS %q", c.s, got, c.serialize)
			}
		}
		out, _ := toAnthropicToolInput(c.s)
		b, _ := json.Marshal(out)
		r85provaSameJSON(t, "anthropic "+c.s, string(b), c.anthropic)
		var args interface{} = c.s
		if v, ok := SecureJSONParse(c.s); ok {
			args = v
		}
		b, _ = json.Marshal(args)
		r85provaSameJSON(t, "google "+c.s, string(b), c.google)
	}
}

// A tool call whose Go input is a JSON string literal holding a JSON object
// is sent as that object, in the key order of the text it holds (TS
// 4ae6986cd8), to Anthropic tool_use.input and Google functionCall.args.
func TestR85provaStringToolInputConverters(t *testing.T) {
	literal, _ := jsonstr.Marshal(`{"b":2,"a":1}`)
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "call_1", ToolName: "search", Input: string(literal)}}},
	}
	ap, err := ConvertToAnthropicPrompt(msgs, AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(ap.Messages)
	if !strings.Contains(string(b), `"input":{"b":2,"a":1}`) {
		t.Errorf("anthropic: %s", b)
	}
	gp, err := ConvertToGoogleMessages(msgs, GoogleMessagesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(gp.Contents)
	if !strings.Contains(string(b), `"args":{"b":2,"a":1}`) {
		t.Errorf("google: %s", b)
	}
}

// google-prompt.ts googleVideoMetadataSchema (TS b1bcea13b8): a user file
// part's providerOptions.google.videoMetadata is validated and sent next to
// fileData / inlineData in schema key order; unknown keys are dropped.
func TestR85provaGoogleVideoMetadata(t *testing.T) {
	video := func(meta interface{}, data types.FileContent) []types.Message {
		data.ProviderOptions = map[string]interface{}{"google": map[string]interface{}{"videoMetadata": meta}}
		return []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{data}}}
	}
	meta := map[string]interface{}{"fps": 2, "endOffset": "10s", "startOffset": "3.5s", "extra": true}
	for name, file := range map[string]types.FileContent{
		"url":   {MediaType: "video/mp4", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/v.mp4"}},
		"bytes": {MediaType: "video/mp4", Data: []byte{1, 2, 3}},
		"text":  {MediaType: "text/plain", FileData: types.FileData{Type: types.FileDataTypeText, Text: "hello"}},
		"ref":   {MediaType: "video/mp4", FileData: types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"google": "files/abc"}}},
	} {
		gp, err := ConvertToGoogleMessages(video(meta, file), GoogleMessagesOptions{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, _ := json.Marshal(gp.Contents)
		if !strings.Contains(string(b), `"videoMetadata":{"startOffset":"3.5s","endOffset":"10s","fps":2}`) {
			t.Errorf("%s: %s", name, b)
		}
	}
	// Assistant file parts ignore it; an absent or null value adds nothing.
	gp, err := ConvertToGoogleMessages(video(nil, types.FileContent{MediaType: "video/mp4", Data: []byte{1}}), GoogleMessagesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(gp.Contents); strings.Contains(string(b), "videoMetadata") {
		t.Errorf("null: %s", b)
	}
	for _, bad := range []interface{}{
		map[string]interface{}{"fps": 0},
		map[string]interface{}{"fps": 24.5},
		map[string]interface{}{"startOffset": 3},
		map[string]interface{}{"endOffset": nil},
		"3s",
	} {
		_, err := ConvertToGoogleMessages(video(bad, types.FileContent{MediaType: "video/mp4", Data: []byte{1}}), GoogleMessagesOptions{})
		if err == nil {
			t.Errorf("%v: want a validation error", bad)
		}
	}
	if _, err := ConvertToGoogleMessages(video(map[string]interface{}{"fps": 24}, types.FileContent{MediaType: "video/mp4", Data: []byte{1}}), GoogleMessagesOptions{}); err != nil {
		t.Errorf("fps 24: %v", err)
	}
}

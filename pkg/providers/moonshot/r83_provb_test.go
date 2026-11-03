package moonshot

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS convert-to-moonshotai-chat-messages.ts 260-268: a text file part is
// new TextDecoder().decode(bytes): BOM dropped, one U+FFFD per invalid subpart.
func TestR83provbMoonshotTextPartUsesTextDecoder(t *testing.T) {
	raw := []byte{0xEF, 0xBB, 0xBF, 0x41, 0xFF, 0xFE, 0x42, 0xE0, 0x80, 0x43}
	want := "A��B��C"
	for _, fd := range []types.FileData{
		{Type: types.FileDataTypeData, Data: raw},
		{Type: types.FileDataTypeData, DataString: base64.StdEncoding.EncodeToString(raw)},
	} {
		part, err := convertMoonshotFilePart(types.FileContent{MediaType: "text/plain", FileData: fd})
		if err != nil {
			t.Fatal(err)
		}
		if part["text"] != want {
			t.Fatalf("text = %q, want %q", part["text"], want)
		}
	}
}

// r83provbMFJSCases are TS normalizeJsonSchemaForMFJS inputs (JSON text) and
// JSON.stringify of the result, run in Node at e64ed82f27: the root union's
// "type" and a branch's moved "type" come first ({ type, ...x }), a tuple's
// prefixItems goes where the assignment adds it, and an empty tuple is [].
var r83provbMFJSCases = []struct{ in, want string }{
	{
		`{"anyOf":[{"type":"object","properties":{"a":{"type":"string"}}},{"type":"object","properties":{"b":{"type":"number"}}}],"$defs":{}}`,
		`{"type":"object","anyOf":[{"type":"object","properties":{"a":{"type":"string"}}},{"type":"object","properties":{"b":{"type":"number"}}}],"$defs":{}}`,
	},
	{
		`{"type":"object","properties":{"x":{"minLength":1,"type":"string","anyOf":[{"maxLength":3},{"type":null,"minimum":2},{"type":"number"}]},"t":{"items":[{"type":"string"}],"type":"array"},"e":{"type":"array","items":[]}}}`,
		`{"type":"object","properties":{"x":{"minLength":1,"anyOf":[{"type":"string","maxLength":3},{"type":null,"minimum":2},{"type":"number"}]},"t":{"type":"array","prefixItems":[{"type":"string"}]},"e":{"type":"array","prefixItems":[]}}}`,
	},
}

// TS moonshotai-prepare-tools.ts (parameters) and
// moonshotai-chat-language-model.ts 402-421 (response_format schema, $schema
// stripped) send the normalized JS object, whose key order is the source's
// plus the keys the normalization adds where the spread/assignment puts them.
func TestR83provbMoonshotMFJSKeyOrderMatchesTS(t *testing.T) {
	model := newMoonshotTestModel("kimi-k2.5")
	for i, c := range r83provbMFJSCases {
		got, err := normalizeMFJSKeepingOrder(json.RawMessage(c.in))
		if err != nil {
			t.Fatal(err)
		}
		if b, _ := json.Marshal(got); string(b) != c.want {
			t.Fatalf("case %d tool parameters:\n got %s\nwant %s", i, b, c.want)
		}
		withDollar := `{"$schema":"http://json-schema.org/draft-07/schema#",` + c.in[1:]
		body, _, err := model.buildRequestBodyWithWarnings(&provider.GenerateOptions{
			Prompt:         textPrompt("hi"),
			ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: json.RawMessage(withDollar)},
		}, false)
		if err != nil {
			t.Fatal(err)
		}
		rf, _ := body["response_format"].(map[string]interface{})
		js, _ := rf["json_schema"].(map[string]interface{})
		if b, _ := json.Marshal(js["schema"]); string(b) != c.want {
			t.Fatalf("case %d response schema:\n got %s\nwant %s", i, b, c.want)
		}
	}
	// A map schema keeps giving a map (no order), with [] for an empty tuple.
	out, err := NormalizeJSONSchemaForMFJS(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"e": map[string]interface{}{"type": "array", "items": []interface{}{}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out.(map[string]interface{}); !ok {
		t.Fatalf("map input gave %T", out)
	}
	if b, _ := json.Marshal(out); string(b) != `{"properties":{"e":{"prefixItems":[],"type":"array"}},"type":"object"}` {
		t.Fatalf("map output %s", b)
	}
}

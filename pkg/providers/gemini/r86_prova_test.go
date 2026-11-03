package gemini

import (
	"encoding/json"
	"testing"
)

// TS googleFailedResponseHandler parses response.text() (BOM dropped).
func TestR86provaGoogleHTTPErrorBOM(t *testing.T) {
	body := []byte("\xEF\xBB\xBF{\"error\":{\"code\":400,\"message\":\"boom\",\"status\":\"INVALID_ARGUMENT\"}}")
	pe := GoogleHTTPError("google", 400, nil, body, nil)
	if pe.Message != "boom" || pe.ErrorCode != "INVALID_ARGUMENT" || pe.ResponseBody != string(body[3:]) {
		t.Fatalf("%#v", pe)
	}
}

// cr-r86-gen-to-prova: the key order of TS sanitizeResponseJsonSchema
// (the other input keys, then enum from const, properties, items,
// additionalProperties, anyOf, oneOf; $defs and an existing enum keep their
// place). Expected values are the TS function's output under node at
// 30548ee7f9 (node imported sanitize-response-json-schema.ts directly).
func TestR86provaSanitizeResponseSchemaKeyOrderMatchesTS(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"{\"type\":\"object\",\"properties\":{\"b\":{\"const\":\"x\",\"type\":\"string\"},\"a\":{\"type\":\"number\"}},\"required\":[\"b\"],\"additionalProperties\":false,\"description\":\"d\"}", "{\"type\":\"object\",\"required\":[\"b\"],\"description\":\"d\",\"properties\":{\"b\":{\"type\":\"string\",\"enum\":[\"x\"]},\"a\":{\"type\":\"number\"}},\"additionalProperties\":false}"},
		{"{\"anyOf\":[{\"type\":\"string\"}],\"title\":\"t\",\"$defs\":{\"X\":{\"const\":1}},\"items\":{\"type\":\"string\"},\"type\":\"array\"}", "{\"title\":\"t\",\"$defs\":{\"X\":{\"enum\":[1]}},\"type\":\"array\",\"items\":{\"type\":\"string\"},\"anyOf\":[{\"type\":\"string\"}]}"},
		{"{\"enum\":[\"a\"],\"const\":\"b\",\"type\":\"string\",\"items\":[{\"const\":1,\"type\":\"number\"},{\"type\":\"string\"}],\"oneOf\":[{\"properties\":{\"z\":{\"type\":\"string\"},\"1\":{\"type\":\"number\"}}}]}", "{\"enum\":[\"b\"],\"type\":\"string\",\"items\":[{\"type\":\"number\",\"enum\":[1]},{\"type\":\"string\"}],\"oneOf\":[{\"properties\":{\"1\":{\"type\":\"number\"},\"z\":{\"type\":\"string\"}}}]}"},
		{"{\"additionalProperties\":{\"const\":true,\"description\":\"x\"},\"type\":\"object\",\"$defs\":{\"b\":{\"type\":\"string\"},\"a\":{\"anyOf\":[{\"const\":null}]}},\"properties\":{\"p\":true}}", "{\"type\":\"object\",\"$defs\":{\"b\":{\"type\":\"string\"},\"a\":{\"anyOf\":[{\"enum\":[null]}]}},\"properties\":{\"p\":true},\"additionalProperties\":{\"description\":\"x\",\"enum\":[true]}}"},
	} {
		got, err := json.Marshal(sanitizeResponseJSONSchema(json.RawMessage(c.in)))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != c.want {
			t.Errorf("in   %s\ngot  %s\nwant %s", c.in, got, c.want)
		}
	}
}

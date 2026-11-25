package tool

import (
	"encoding/json"
	"testing"
)

// TS sanitize-json-schema.ts formatConstraintValue is JSON.stringify(value):
// an object-valued constraint (most often `not`) keeps the key order the
// caller wrote. The expected text is the TS sanitizer's output for the same
// raw schema (node, @ai-sdk/anthropic at 382d0b8eaf). A randomized
// differential against the TS sanitizer (12,000 schemas, round 89) differed
// only in this key order; it matches after the fix.
func TestR89genAnthropicConstraintValueKeyOrder(t *testing.T) {
	in := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","not":{"type":"string","enum":["admin","root"]},"maxLength":{"b":1,"a":[{"z":1,"y":2}]}},"tags":{"type":"array","items":{"type":"string","not":{"pattern":"^x","minLength":3}}}},"$defs":{"Z":{"type":"number","not":{"multipleOf":2,"maximum":1.5}}},"anyOf":[{"type":"object","not":{"required":["q"],"properties":{"q":{"type":"null"}}}}]}`)
	want := `{"type":"object","anyOf":[{"type":"object","additionalProperties":false,"description":"not: {\"required\":[\"q\"],\"properties\":{\"q\":{\"type\":\"null\"}}}."}],"$defs":{"Z":{"type":"number","description":"not: {\"multipleOf\":2,\"maximum\":1.5}."}},"properties":{"name":{"type":"string","description":"max length: {\"b\":1,\"a\":[{\"z\":1,\"y\":2}]}; not: {\"type\":\"string\",\"enum\":[\"admin\",\"root\"]}."},"tags":{"type":"array","items":{"type":"string","description":"not: {\"pattern\":\"^x\",\"minLength\":3}."}}},"additionalProperties":false}`
	got, err := json.Marshal(SanitizeAnthropicSchema(in))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

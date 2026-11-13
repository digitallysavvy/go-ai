package zodlite

import (
	"encoding/json"
	"testing"
)

// zod walks a record, a catchall and a strict object's unknown keys in the
// JS object's key order: array-index keys ascending, then the others in wire
// order (a duplicate keeps its first position). Expected messages are node
// safeParseJSON over these frames (zod 4, provider-utils at 2ebc78b654).
const r97carryTSKeyOrderMessages = `{"catch":"Type validation failed: Value: {\"2\":\"v\",\"10\":\"y\",\"z\":\"u\",\"a\":2,\"b\":\"w\"}.\nError message: [\n  {\n    \"expected\": \"number\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"2\"\n    ],\n    \"message\": \"Invalid input: expected number, received string\"\n  },\n  {\n    \"expected\": \"number\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"10\"\n    ],\n    \"message\": \"Invalid input: expected number, received string\"\n  },\n  {\n    \"expected\": \"number\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"z\"\n    ],\n    \"message\": \"Invalid input: expected number, received string\"\n  },\n  {\n    \"expected\": \"number\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"b\"\n    ],\n    \"message\": \"Invalid input: expected number, received string\"\n  }\n]","record":"Type validation failed: Value: {\"2\":\"v\",\"10\":\"y\",\"z\":\"x\",\"b\":\"w\",\"01\":\"t\"}.\nError message: [\n  {\n    \"expected\": \"number\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"2\"\n    ],\n    \"message\": \"Invalid input: expected number, received string\"\n  },\n  {\n    \"expected\": \"number\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"10\"\n    ],\n    \"message\": \"Invalid input: expected number, received string\"\n  },\n  {\n    \"expected\": \"number\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"z\"\n    ],\n    \"message\": \"Invalid input: expected number, received string\"\n  },\n  {\n    \"expected\": \"number\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"b\"\n    ],\n    \"message\": \"Invalid input: expected number, received string\"\n  },\n  {\n    \"expected\": \"number\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"01\"\n    ],\n    \"message\": \"Invalid input: expected number, received string\"\n  }\n]","strict":"Type validation failed: Value: {\"2\":5,\"10\":3,\"z\":1,\"a\":2,\"b\":4}.\nError message: [\n  {\n    \"code\": \"unrecognized_keys\",\n    \"keys\": [\n      \"2\",\n      \"10\",\n      \"z\",\n      \"b\"\n    ],\n    \"path\": [],\n    \"message\": \"Unrecognized keys: \\\"2\\\", \\\"10\\\", \\\"z\\\", \\\"b\\\"\"\n  }\n]","nested":"Type validation failed: Value: {\"u\":{\"q\":{\"n\":\"x\",\"y\":1,\"c\":2},\"b\":{\"n\":1,\"k\":3,\"a\":4}}}.\nError message: [\n  {\n    \"expected\": \"number\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"u\",\n      \"q\",\n      \"n\"\n    ],\n    \"message\": \"Invalid input: expected number, received string\"\n  },\n  {\n    \"expected\": \"string\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"u\",\n      \"q\",\n      \"y\"\n    ],\n    \"message\": \"Invalid input: expected string, received number\"\n  },\n  {\n    \"expected\": \"string\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"u\",\n      \"q\",\n      \"c\"\n    ],\n    \"message\": \"Invalid input: expected string, received number\"\n  },\n  {\n    \"expected\": \"string\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"u\",\n      \"b\",\n      \"k\"\n    ],\n    \"message\": \"Invalid input: expected string, received number\"\n  },\n  {\n    \"expected\": \"string\",\n    \"code\": \"invalid_type\",\n    \"path\": [\n      \"u\",\n      \"b\",\n      \"a\"\n    ],\n    \"message\": \"Invalid input: expected string, received number\"\n  }\n]"}`

func TestR97carryIssuesInJSKeyOrder(t *testing.T) {
	var want map[string]string
	if err := json.Unmarshal([]byte(r97carryTSKeyOrderMessages), &want); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, data string
		node       Node
	}{
		{"catch", `{"z":"x","a":2,"10":"y","b":"w","2":"v","z":"u"}`, ObjectCatchall(Number, F("a", Optional(Number)))},
		{"record", `{"z":"x","10":"y","b":"w","2":"v","01":"t"}`, RecordOf(Number)},
		{"strict", `{"z":1,"a":2,"10":3,"b":4,"2":5}`, StrictObject(F("a", Number))},
		{"nested", `{"u":{"q":{"n":"x","y":1,"c":2},"b":{"n":1,"k":3,"a":4}}}`, Object(F("u", RecordOf(ObjectCatchall(String, F("n", Number)))))},
	} {
		if got := ValidationMessage(tc.node, tc.data); got != want[tc.name] {
			t.Errorf("%s:\nGo %s\nTS %s", tc.name, got, want[tc.name])
		}
	}
}

// A node called on a decoded map without Validate still reports every issue,
// keys sorted with array-index keys first.
func TestR97carryIssuesWithoutValidateSorted(t *testing.T) {
	var v interface{}
	_ = json.Unmarshal([]byte(`{"z":"x","10":"y","b":"w","2":"v"}`), &v)
	bad := RecordOf(Number)(v, true, nil)
	var paths []string
	for _, it := range bad.all() {
		paths = append(paths, it.Path[0].(string))
	}
	if got := len(paths); got != 4 || paths[0] != "2" || paths[1] != "10" || paths[2] != "b" || paths[3] != "z" {
		t.Fatalf("paths = %v", paths)
	}
}

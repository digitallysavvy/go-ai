package zodlite

import (
	"strings"
	"testing"
)

func r96carryIssues(bad *Issue) string {
	items := []string{}
	for _, it := range bad.all() {
		items = append(items, string(it.json()))
	}
	return "[" + strings.Join(items, ",") + "]"
}

// Issues recorded from zod 4.6.7 (node): the providers' jsonValueSchema
// intersection (OpenAI Responses usage) and z.json() catchall (xAI usage)
// reject a number JSON.parse read as Infinity, and z.number() adds
// "received".
func TestR96carryJSONValueIssuesMatchZod(t *testing.T) {
	jo := RecordOf(JSON)
	usage := Intersection(jo, Object(F("input_tokens", Number), F("output_tokens", Number)))
	chunk := Object(F("type", Literal("x")), F("response", Object(F("usage", Nullish(usage)))))
	xu := ObjectCatchall(JSON, F("input_tokens", Number), F("d", Optional(ObjectCatchall(JSON, F("c", Optional(Number))))))
	cases := []struct {
		name   string
		schema Node
		data   string
		want   string
	}{
		{"intersection extra key", chunk, `{"type":"x","response":{"usage":{"input_tokens":1,"output_tokens":2,"foo":1e400}}}`, "[{\"code\":\"invalid_union\",\"errors\":[[{\"expected\":\"string\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected string, received Infinity\"}],[{\"expected\":\"number\",\"code\":\"invalid_type\",\"received\":\"Infinity\",\"path\":[],\"message\":\"Invalid input: expected number, received Infinity\"}],[{\"expected\":\"boolean\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected boolean, received Infinity\"}],[{\"expected\":\"null\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected null, received Infinity\"}],[{\"expected\":\"array\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected array, received Infinity\"}],[{\"expected\":\"record\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected record, received Infinity\"}]],\"path\":[\"response\",\"usage\",\"foo\"],\"message\":\"Invalid input\"}]"},
		{"intersection both members", chunk, `{"type":"x","response":{"usage":{"input_tokens":1e400,"output_tokens":2}}}`, "[{\"code\":\"invalid_union\",\"errors\":[[{\"expected\":\"string\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected string, received Infinity\"}],[{\"expected\":\"number\",\"code\":\"invalid_type\",\"received\":\"Infinity\",\"path\":[],\"message\":\"Invalid input: expected number, received Infinity\"}],[{\"expected\":\"boolean\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected boolean, received Infinity\"}],[{\"expected\":\"null\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected null, received Infinity\"}],[{\"expected\":\"array\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected array, received Infinity\"}],[{\"expected\":\"record\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected record, received Infinity\"}]],\"path\":[\"response\",\"usage\",\"input_tokens\"],\"message\":\"Invalid input\"},{\"expected\":\"number\",\"code\":\"invalid_type\",\"received\":\"Infinity\",\"path\":[\"response\",\"usage\",\"input_tokens\"],\"message\":\"Invalid input: expected number, received Infinity\"}]"},
		{"catchall nested", xu, `{"input_tokens":1,"d":{"x":[1e400]}}`, "[{\"code\":\"invalid_union\",\"errors\":[[{\"expected\":\"string\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected string, received array\"}],[{\"expected\":\"number\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected number, received array\"}],[{\"expected\":\"boolean\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected boolean, received array\"}],[{\"expected\":\"null\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected null, received array\"}],[{\"code\":\"invalid_union\",\"errors\":[[{\"expected\":\"string\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected string, received Infinity\"}],[{\"expected\":\"number\",\"code\":\"invalid_type\",\"received\":\"Infinity\",\"path\":[],\"message\":\"Invalid input: expected number, received Infinity\"}],[{\"expected\":\"boolean\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected boolean, received Infinity\"}],[{\"expected\":\"null\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected null, received Infinity\"}],[{\"expected\":\"array\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected array, received Infinity\"}],[{\"expected\":\"record\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected record, received Infinity\"}]],\"path\":[0],\"message\":\"Invalid input\"}],[{\"expected\":\"record\",\"code\":\"invalid_type\",\"path\":[],\"message\":\"Invalid input: expected record, received array\"}]],\"path\":[\"d\",\"x\"],\"message\":\"Invalid input\"}]"},
	}
	for _, tc := range cases {
		var value interface{}
		if err := decodeForTest(tc.data, &value); err != nil {
			t.Fatal(err)
		}
		bad := tc.schema(value, true, nil)
		if bad == nil {
			t.Errorf("%s: passed", tc.name)
			continue
		}
		if got := r96carryIssues(bad); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
	var ok interface{}
	_ = decodeForTest(`{"input_tokens":1,"foo":{"a":1}}`, &ok)
	if bad := xu(ok, true, nil); bad != nil {
		t.Errorf("finite catchall failed: %v", bad)
	}
}

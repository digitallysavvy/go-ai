package prompt

import "testing"

// V061 item 28 (cr-r86b-provb-to-prova 1, cr-r86b-prova-to-integrator 2):
// TS serializeToolCallArguments returns stringifyWithBigIntAndCycles(input)
// for a non-string input, JSON.stringify of the parsed tool input. Wants
// printed by Node 24 from a literal copy of the TS function (provb).
func TestR86bprovaSerializeToolCallArgumentsRestringified(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"{\"b\": 1, \"a\": [1.0, 2e0]}", "{\"b\":1,\"a\":[1,2]}"},
		{"{\"2\":\"x\",\"1\":\"y\",\"k\":-0}", "{\"1\":\"y\",\"2\":\"x\",\"k\":0}"},
		{"[1, {\"z\": 1, \"y\": 2.50}]", "[1,{\"z\":1,\"y\":2.5}]"},
		{"\"{\\\"q\\\": 1.0}\"", "{\"q\": 1.0}"},
		{"\"\\u0061bc\"", "\"abc\""},
		{"not json", "\"not json\""},
		{"   ", "\"   \""},
		{"\u00a0{\"a\":1}", "\"\u00a0{\\\"a\\\":1}\""},
		{"12.0", "12"},
		{"{\"a\":1,\"a\":2}", "{\"a\":2}"},
		{" {\"a\" : 1e2} ", "{\"a\":100}"},
	} {
		if got := SerializeToolCallArguments(tc.raw); got != tc.want {
			t.Errorf("SerializeToolCallArguments(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

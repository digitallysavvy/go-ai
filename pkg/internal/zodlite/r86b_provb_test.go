package zodlite

import "testing"

// TestR86bprovbCompactJSONIsJSStringifyOfParse pins CompactJSON to
// JSON.stringify(JSON.parse(x)); every want was printed by Node 24
// (V061 item 28: TS re-stringifies a parsed tool input).
func TestR86bprovbCompactJSONIsJSStringifyOfParse(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"{\"a\": 1}", "{\"a\":1}"},
		{"{\"b\":1,\"2\":0,\"1\":0}", "{\"1\":0,\"2\":0,\"b\":1}"},
		{"[1.0, 1e2, -0, 1e400, 0.1e-7, 123456789012345678901234, 5e-324, 1.7976931348623157e308]", "[1,100,0,null,1e-8,1.2345678901234569e+23,5e-324,1.7976931348623157e+308]"},
		{"{\"a\":1,\"a\":2,\"b\":3}", "{\"a\":2,\"b\":3}"},
		{"\"\\u0061\\/\"", "\"a/\""},
		{"{\"s\":\"<>&\\u2028\\u2029\\u0007\\b\\f\\u001f\\u007f\"}", "{\"s\":\"<>&\u2028\u2029\\u0007\\b\\f\\u001f\u007f\"}"},
		{"{\"__proto__\":1,\"constructor\":{\"prototype\":2}}", "{\"__proto__\":1,\"constructor\":{\"prototype\":2}}"},
		{"  true ", "true"},
		{"null", "null"},
		{"{\"x\":[{\"z\":1,\"y\":2}]}", "{\"x\":[{\"z\":1,\"y\":2}]}"},
		{"{\"10\":1,\"9\":2,\"01\":3,\"4294967295\":4,\"4294967294\":5,\"-1\":6}", "{\"9\":2,\"10\":1,\"4294967294\":5,\"01\":3,\"4294967295\":4,\"-1\":6}"},
		{"1E21", "1e+21"},
		{"-1.5e-7", "-1.5e-7"},
		{"1e-7", "1e-7"},
		{"0.000001", "0.000001"},
		{"100000000000000000000", "100000000000000000000"},
		{"{ \"query\" : \"weather in \\\"Paris\\\"\" , \"n\" : 10.50 }", "{\"query\":\"weather in \\\"Paris\\\"\",\"n\":10.5}"},
		{"not json", "not json"},
		{"{\"a\":1} trailing", "{\"a\":1} trailing"},
	} {
		if got := CompactJSON(tc.in); got != tc.want {
			t.Errorf("CompactJSON(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

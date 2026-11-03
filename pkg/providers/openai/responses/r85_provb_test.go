package responses

import "testing"

// TS 4ae6986cd8 serializeToolCallArguments: valid JSON input text is sent
// as is; other text as its JSON string literal.
func TestR85provbResponsesFunctionCallArguments(t *testing.T) {
	for raw, want := range map[string]string{
		`{"a":1}`:  `{"a":1}`,
		`[1, 2]`:   `[1,2]`,
		`"x"`:      `"x"`,
		`5`:        `5`,
		`not json`: `"not json"`,
		`{"a":`:    `"{\"a\":"`,
		``:         `{}`,
	} {
		if got := responsesFunctionCallArguments(nil, raw); got != want {
			t.Errorf("%q: got %s, want %s", raw, got, want)
		}
	}
}

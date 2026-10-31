package bedrock

import "testing"

// TS AmazonBedrockErrorSchema: message is a required string (empty is valid)
// and type is nullish, so only null/absent type drops the "type: " prefix.
func TestBedrockAPIErrorEmptyStringFieldsLikeTS(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{`{"message":"boom"}`, "boom"},
		{`{"message":"boom","type":null}`, "boom"},
		{`{"message":"boom","type":"Throttling"}`, "Throttling: boom"},
		{`{"message":"boom","type":""}`, ": boom"},
		{`{"message":""}`, ""},
		{`{"type":"X"}`, "Bad Request"},
	}
	for _, c := range cases {
		if got := bedrockAPIError(400, []byte(c.body), nil).Message; got != c.want {
			t.Errorf("%s: message = %q, want %q", c.body, got, c.want)
		}
	}
	if got := bedrockConverseGenerateAPIError(400, []byte(`{"message":"","type":"X"}`), nil).Message; got != "" {
		t.Errorf("converse message = %q, want empty", got)
	}
}

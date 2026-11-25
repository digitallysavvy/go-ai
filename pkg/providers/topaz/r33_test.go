package topaz

import "testing"

// z.array(z.object({msg}).loose()) rejects a null entry, so the body is off
// schema and the HTTP status text is the message.
func TestR33_NullIssueEntryIsOffSchema(t *testing.T) {
	for _, body := range []string{`{"detail":[null],"message":"m"}`, `{"errors":[null],"message":"m"}`} {
		err := newTopazAPIError(400, []byte(body), nil)
		if got := err.Error(); !contains(got, "Bad Request") || contains(got, ": m") {
			t.Errorf("%s -> %q", body, got)
		}
	}
	err := newTopazAPIError(400, []byte(`{"errors":[{"msg":"a"}],"message":"m"}`), nil)
	if !contains(err.Error(), "m: a") {
		t.Errorf("valid body -> %q", err.Error())
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

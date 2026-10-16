package bfl

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestBFLStatusErrorFollowsTSHandler(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		wantMsg   string
		wantRetry bool
	}{
		{"detail string", 400, `{"detail":"bad prompt"}`, "bad prompt", false},
		{"detail array is compact JSON", 422, `{"detail": [ {"loc":["body","prompt"], "msg":"required"} ]}`, `[{"loc":["body","prompt"],"msg":"required"}]`, false},
		{"message", 500, `{"message":"boom"}`, "boom", true},
		{"status phrase", 503, `{"status":"Failed"}`, "Black Forest Labs generation failed: Failed.", false},
		{"state phrase non terminal", 503, `{"state":"Pending"}`, "Black Forest Labs generation failed: Pending.", true},
		{"unknown", 500, `{}`, "Unknown Black Forest Labs error", true},
		{"not json", 502, `oops`, "Bad Gateway", true},
		{"empty", 429, ``, "Too Many Requests", true},
		{"wrong type is off schema", 400, `{"message":5}`, "Bad Request", false},
		{"terminal status beats message", 503, `{"message":"x","status":"Request Moderated"}`, "x", false},
	}
	for _, c := range cases {
		err := bflStatusError(c.status, nil, []byte(c.body))
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) {
			t.Fatalf("%s: %T", c.name, err)
		}
		if perr.Message != c.wantMsg {
			t.Errorf("%s: message = %q, want %q", c.name, perr.Message, c.wantMsg)
		}
		if perr.StatusCode != c.status {
			t.Errorf("%s: status = %d", c.name, perr.StatusCode)
		}
		if perr.IsRetryable() != c.wantRetry {
			t.Errorf("%s: retryable = %v, want %v", c.name, perr.IsRetryable(), c.wantRetry)
		}
	}
}

func TestDescribePollDetailsIsCompactJSON(t *testing.T) {
	got := describePollDetails([]byte("{ \"reason\": \"nsfw\",\n  \"scores\": [1, 2] }"))
	if want := `{"reason":"nsfw","scores":[1,2]}`; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if got := describePollDetails([]byte(`"plain"`)); got != "plain" {
		t.Fatalf("string details = %q", got)
	}
}

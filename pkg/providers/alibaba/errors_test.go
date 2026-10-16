package alibaba

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS alibabaFailedResponseHandler (chat) and the video/embedding handlers:
// bodies matching the schema give their message and code, anything else the
// HTTP status text.
func TestAlibabaErrorSchemas(t *testing.T) {
	type tc struct {
		name, body, wantMsg, wantCode string
		status                        int
	}
	for _, c := range []tc{
		{"chat schema", `{"error":{"message":"bad key","code":"invalid_api_key","type":"t"}}`, "bad key", "invalid_api_key", 401},
		{"chat flat message is off schema", `{"message":"bad"}`, "Unauthorized", "", 401},
		{"chat numeric code is off schema", `{"error":{"message":"bad","code":5}}`, "Unauthorized", "", 401},
		{"chat plain text", `oops`, "Bad Gateway", "", 502},
	} {
		msg, code := alibabaChatErrorMessage(c.status, []byte(c.body))
		if msg != c.wantMsg || code != c.wantCode {
			t.Errorf("%s: got (%q, %q) want (%q, %q)", c.name, msg, code, c.wantMsg, c.wantCode)
		}
	}
	for _, c := range []tc{
		{"native schema", `{"code":"InvalidParameter","message":"bad","request_id":"r"}`, "bad", "InvalidParameter", 400},
		{"native message only", `{"message":"bad"}`, "bad", "", 400},
		{"native nested error is off schema", `{"error":{"message":"bad"}}`, "Bad Request", "", 400},
		{"native plain text", `oops`, "Bad Gateway", "", 502},
	} {
		msg, code := alibabaNativeErrorMessage(c.status, []byte(c.body))
		if msg != c.wantMsg || code != c.wantCode {
			t.Errorf("%s: got (%q, %q) want (%q, %q)", c.name, msg, code, c.wantMsg, c.wantCode)
		}
	}
	err := (&LanguageModel{}).handleError(&internalhttp.HTTPStatusError{StatusCode: 429, Body: []byte(`{"error":{"message":"slow"}}`)})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "slow" || perr.StatusCode != 429 || !perr.IsRetryable() {
		t.Fatalf("chat handleError = %v", err)
	}
}

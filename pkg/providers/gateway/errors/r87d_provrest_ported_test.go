package errors

import (
	stderrors "errors"
	"fmt"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
)

// Hand-port marks for create-gateway-error.test.ts (R87D_TSFIXTURE closure
// rule). The five `expect(responseError.response).toBe(response)` assertions
// are n/a: reference equality in TS, while CreateGatewayErrorFromResponse
// takes the raw body bytes (TestTSCasesCreateGatewayError compares the kept
// value with the TS response instead). The two `expect(error.cause).toBe(originalCause)` assertions keep
// their identity in Go: TestR87dprovrestGatewayErrorCauseIdentity.
var r87dprovrestPorted = map[string]string{}

// r87dprovrestNA are the assertions that cannot apply to Go.
var r87dprovrestNA = map[string]string{}

func init() {
	for _, test := range []string{
		"should use defaultMessage when response message is null",
		"should create GatewayResponseError for completely invalid response",
		"should create GatewayResponseError for missing error field",
		"should create GatewayResponseError for array response",
		"should use default defaultMessage when not provided",
	} {
		r87dprovrestNA[tsfixture.PortedKey(test, "responseError.response", "toBe")] = "toBe is reference identity with the caller's response object; CreateGatewayErrorFromResponse takes the body bytes, so Go cannot hand back the caller's object (TestTSCasesCreateGatewayError compares the kept value with the TS response)"
	}
	for _, test := range []string{
		"should include cause in the created error",
		"should preserve error properties correctly",
	} {
		r87dprovrestPorted[tsfixture.PortedKey(test, "error.cause", "toBe")] = "TestR87dprovrestGatewayErrorCauseIdentity"
	}
	tsfixture.RegisterPorted(r87dprovrestPorted)
	tsfixture.RegisterNotApplicable(r87dprovrestNA)
}

// TestMain fails an unfiltered run whose hand-port or n/a marks are stale.
func TestMain(m *testing.M) { os.Exit(tsfixture.PortedMain(m, r87dprovrestPorted, r87dprovrestNA)) }

// create-gateway-error.test.ts 306-322 and 508-530: the created error's cause
// is the caller's cause (toBe: the same error value).
func TestR87dprovrestGatewayErrorCauseIdentity(t *testing.T) {
	for _, tc := range []struct {
		body           string
		status         int
		defaultMessage string
		cause          error
	}{
		{`{"error":{"message":"Gateway timeout","type":"internal_server_error"}}`, 504, "Gateway request failed", stderrors.New("Original network error")},
		{`{"error":{"message":"Rate limit hit","type":"rate_limit_exceeded"}}`, 429, "Fallback message", fmt.Errorf("Type error")},
	} {
		err := CreateGatewayErrorFromResponse([]byte(tc.body), tc.status, tc.defaultMessage, tc.cause, "")
		if got := stderrors.Unwrap(err); got != tc.cause {
			t.Errorf("%d: error.cause = %v (%p), want the caller's cause %p", tc.status, got, got, tc.cause)
		}
	}
}

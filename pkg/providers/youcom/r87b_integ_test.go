package youcom

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// V061 46: the 402 message reads `body.message || default`,
// `body.upgrade_url || default` and `body.reset_at` truthiness (JS `||`,
// template String()): an empty string falls back, any other truthy value is
// interpolated as JS String(value).
func TestR87bintegPaymentRequiredMessageJSOr(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{}`, "Free tier limit exceeded. Please upgrade to continue. Upgrade at: https://you.com/platform"},
		{`{"message":"","upgrade_url":"","reset_at":""}`, "Free tier limit exceeded. Please upgrade to continue. Upgrade at: https://you.com/platform"},
		{`{"message":"Out of credits.","upgrade_url":"https://u","reset_at":"2026-12-01"}`, "Out of credits. Limit resets on 2026-12-01. Upgrade at: https://u"},
		{`{"message":5,"reset_at":0,"upgrade_url":true}`, "5 Upgrade at: true"},
		{`{"message":null,"reset_at":1}`, "Free tier limit exceeded. Please upgrade to continue. Limit resets on 1. Upgrade at: https://you.com/platform"},
		{`not json`, "Free tier limit exceeded. Please upgrade to continue. Upgrade at: https://you.com/platform"},
	} {
		resp := &http.Response{StatusCode: http.StatusPaymentRequired, Body: io.NopCloser(strings.NewReader(tc.body))}
		if got := youAPIHTTPError(endpointSearch, resp).Error(); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.body, got, tc.want)
		}
	}
}

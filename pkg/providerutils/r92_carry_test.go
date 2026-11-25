package providerutils

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createStatusCodeErrorResponseHandler: responseHeaders is
// extractResponseHeaders(response), lower-case keys.
func TestR92carryStatusCodeDownloadErrorHeaderKeys(t *testing.T) {
	err := StatusCodeDownloadError("luma", &providererrors.DownloadError{
		URL: "https://x/y.png", StatusCode: 503, StatusText: "Service Unavailable",
		Headers: map[string][]string{"Retry-After": {"3"}, "X-Request-Id": {"r1"}},
		Body:    []byte(`{"error":{"message":"busy"}}`),
	})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %T %v", err, err)
	}
	if pe.ResponseHeaders["retry-after"] != "3" || pe.ResponseHeaders["x-request-id"] != "r1" {
		t.Fatalf("headers = %#v, want lower-case keys", pe.ResponseHeaders)
	}
	if _, canonical := pe.ResponseHeaders["Retry-After"]; canonical {
		t.Fatalf("headers kept a Go canonical key: %#v", pe.ResponseHeaders)
	}
	if pe.Message != "Service Unavailable" || pe.Data != nil || pe.StatusCode != 503 || !pe.IsRetryable() {
		t.Fatalf("error = %+v", pe)
	}
}

// zod v4 z.url().startsWith('https://'): the startsWith check sees z.url()'s
// output, the trimmed value without tabs and newlines.
func TestR92carryOptURLHTTPSOnlyChecksZodOutput(t *testing.T) {
	check := OptURL(true)
	for _, ok := range []string{"https://x.dev/a", "  https://x.dev/a\n", "\thttps://x.dev/a", "ht\ntps://x.dev"} {
		if err := check("u", ok); err != nil {
			t.Fatalf("OptURL(true)(%q) = %v", ok, err)
		}
	}
	if err := check("u", " http://x.dev"); err == nil {
		t.Fatal("http accepted")
	}
	if got := ZodURLValue(" \thttps://exa\nmple.com/p\r "); got != "https://example.com/p" {
		t.Fatalf("ZodURLValue = %q", got)
	}
}

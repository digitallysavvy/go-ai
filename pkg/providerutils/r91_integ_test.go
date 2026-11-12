package providerutils

import (
	"errors"
	"regexp"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// core2-06 (TS response-handler.ts:459, message: response.statusText): the
// ProviderError message is the server's reason phrase.
func TestR91integStatusCodeDownloadErrorReasonPhrase(t *testing.T) {
	err := StatusCodeDownloadError("p", providererrors.NewDownloadError("https://x/a", 404, "Custom Reason", "", nil))
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Custom Reason" || pe.StatusCode != 404 {
		t.Fatalf("err = %#v", err)
	}
	err = StatusCodeDownloadError("p", providererrors.NewDownloadError("https://x/a", 404, "", "", nil))
	if !errors.As(err, &pe) || pe.Message != "Not Found" {
		t.Fatalf("no reason phrase: err = %#v", err)
	}
}

// core2-11 (TS is-url-supported.ts:36, key.replace(/\*/, '')): only the
// first '*' of a media type key is removed.
func TestR91integMatchesSupportedURLReplacesFirstStar(t *testing.T) {
	supported := map[string][]*regexp.Regexp{"image/**": {regexp.MustCompile(`.*`)}}
	if MatchesSupportedURL(supported, "image/png", "https://x/a.png") {
		t.Fatal(`"image/**" must not match image/png (TS prefix "image/*")`)
	}
	if !MatchesSupportedURL(map[string][]*regexp.Regexp{"IMAGE/*": {regexp.MustCompile(`.*`)}}, "image/png", "https://x/a.png") {
		t.Fatal(`"IMAGE/*" must match image/png (TS lowercases the key)`)
	}
}

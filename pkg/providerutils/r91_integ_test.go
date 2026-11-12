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

// core2-23 (node: Number(), zod 4.6.7 z.number().int() / z.url()).
func TestR91integOptionSchemaJSCoercion(t *testing.T) {
	for _, k := range []string{"1_000", "-0x10", "0x1p-2", "12\u0085", "Infinity", "abc"} {
		if OptKeyNumeric(k) == nil {
			t.Errorf("OptKeyNumeric(%q) accepted", k)
		}
	}
	for _, k := range []string{"", " 42 ", "0x10", "1e3", "-7"} {
		if err := OptKeyNumeric(k); err != nil {
			t.Errorf("OptKeyNumeric(%q) = %v", k, err)
		}
	}
	if OptInt(nil, nil)("p", 1e20) == nil {
		t.Error("OptInt accepted 1e20")
	}
	if err := OptInt(nil, nil)("p", float64(1<<53-1)); err != nil {
		t.Errorf("OptInt(2^53-1) = %v", err)
	}
	for _, u := range []string{"file:///tmp/x", "http:/x", " https://a.com"} {
		if err := OptURL(false)("p", u); err != nil {
			t.Errorf("OptURL(%q) = %v", u, err)
		}
	}
	for _, u := range []string{"not a url", "", "//x"} {
		if OptURL(false)("p", u) == nil {
			t.Errorf("OptURL(%q) accepted", u)
		}
	}
}

package quiverai

import (
	"errors"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// The failed response keeps its body, data and headers (APICallError) on the
// image model and on the Responses handler, with the 429/5xx retry rule.
func TestFailedResponsesKeepBodyDataAndHeaders(t *testing.T) {
	body := `{"status":400,"code":"bad","message":"m","request_id":"r"}`
	httpErr := func() *internalhttp.HTTPStatusError {
		return &internalhttp.HTTPStatusError{StatusCode: 409, Headers: http.Header{"X-Request-Id": {"r1"}}, Body: []byte(body)}
	}
	for name, got := range map[string]error{
		"image":     (&ImageModel{}).handleError(httpErr()),
		"responses": quiverAIFailedResponseHandler(httpErr()),
	} {
		var perr *providererrors.ProviderError
		if !errors.As(got, &perr) || perr.ResponseBody != body || perr.Data == nil || perr.ResponseHeaders["X-Request-Id"] != "r1" ||
			perr.Message != "m" || perr.IsRetryable() {
			t.Errorf("%s: got %#v", name, perr)
		}
	}
}

// TextDecoder drops a leading BOM and trimStart/`\s` are the JS whitespace
// sets, so a BOM or NBSP before the root is still an SVG for animate.
func TestAnimationSourceAcceptsBOMAndJSWhitespace(t *testing.T) {
	for name, data := range map[string]string{
		"bom":  "\ufeff<svg xmlns='http://www.w3.org/2000/svg'/>",
		"nbsp": "\u00a0<svg xmlns='http://www.w3.org/2000/svg'/>",
		"vt":   "<?xml version='1.0'?>\v<svg xmlns='http://www.w3.org/2000/svg'/>",
	} {
		if _, err := toQuiverAIAnimationSource(provider.ImageFile{Type: "file", Data: []byte(data)}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// JS trim (and `\s`) does not strip U+0085, so a trailing NEL after </svg> is
// not an SVG reference image.
func TestReferenceSVGTrailingNELIsNotSVG(t *testing.T) {
	if isSVGBytes([]byte("<svg xmlns='x'></svg>\u0085")) {
		t.Fatal("trailing NEL accepted")
	}
	if !isSVGBytes([]byte("<svg xmlns='x'></svg>\u00a0\ufeff")) {
		t.Fatal("trailing JS whitespace rejected")
	}
}

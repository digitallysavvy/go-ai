package perplexity

import (
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createJsonErrorResponseHandler parses response.text() (BOM dropped).
func TestR86provaPerplexityErrorBodyBOM(t *testing.T) {
	body := []byte("\xEF\xBB\xBF{\"error\":{\"message\":\"boom\",\"type\":\"invalid_request\"}}")
	err := perplexityHTTPError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: body}, "perplexity")
	pe, ok := err.(*providererrors.ProviderError)
	if !ok || pe.Message == "Bad Request" || pe.ResponseBody != string(body[3:]) || pe.Data == nil {
		t.Fatalf("%#v", err)
	}
}

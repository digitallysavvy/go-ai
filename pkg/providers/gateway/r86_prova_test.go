package gateway

import (
	"context"
	"strings"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
)

// TS createJsonErrorResponseHandler keeps response.text() (BOM dropped) as
// the APICallError responseBody that asGatewayError parses.
func TestR86provaGatewayErrorBodyBOM(t *testing.T) {
	p, err := New(Config{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	got := p.gatewayAPIErrorWithContext(context.Background(), &internalhttp.Response{
		StatusCode: 400,
		Body:       []byte("\xEF\xBB\xBF{\"error\":{\"message\":\"boom\",\"type\":\"invalid_request_error\"}}"),
	})
	if got == nil || !strings.Contains(got.Error(), "boom") || strings.Contains(got.Error(), "Invalid error response format") {
		t.Fatalf("%v", got)
	}
}

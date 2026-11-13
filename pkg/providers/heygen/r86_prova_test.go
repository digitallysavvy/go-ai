package heygen

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createJsonErrorResponseHandler parses response.text() (BOM dropped).
func TestR86provaHeyGenErrorBodyBOM(t *testing.T) {
	err := newHeyGenAPIError(400, []byte("\xEF\xBB\xBF{\"error\":{\"code\":\"bad\",\"message\":\"boom\"}}"), nil)
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "boom (bad)" {
		t.Fatalf("%#v", err)
	}
}

package cerebras

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS cerebrasErrorSchema: message, type, param and code are required
// strings; errorToMessage returns message, so "" is kept (provb CR, class of
// 91b2205f).
func TestR87integCerebrasErrorSchema(t *testing.T) {
	err := parseCerebrasHTTPStatusError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(`{"message":"","type":"t","param":"p","code":"c"}`)}, nil)
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "" || pe.ErrorCode != "c" {
		t.Errorf("empty message: got %#v", err)
	}
	for _, body := range []string{`{"message":"m","type":"t"}`, `{"message":"m","type":"t","param":null,"code":"c"}`, `{"type":"t","param":"p","code":"c"}`} {
		// r90: a body that fails the schema is the status-text APICallError
		// without data (TS createJsonErrorResponseHandler).
		err := parseCerebrasHTTPStatusError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)}, nil)
		if !errors.As(err, &pe) || pe.Message != "Bad Request" || pe.Data != nil {
			t.Errorf("%s: got %#v, want the status text without data", body, err)
		}
	}
}

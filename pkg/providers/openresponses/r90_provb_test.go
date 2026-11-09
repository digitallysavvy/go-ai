package openresponses

import (
	"errors"
	"reflect"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR90provbErrorDataIsSchemaOutput: TS createJsonErrorResponseHandler
// sets APICallError.data to the error schema output (undeclared keys
// stripped) for a matching body and leaves it undefined otherwise (the
// provc class of 252b3d76).
func TestR90provbErrorDataIsSchemaOutput(t *testing.T) {
	check := func(name string, err error, want interface{}) {
		t.Helper()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) {
			t.Fatalf("%s: err = %T %v", name, err, err)
		}
		if !reflect.DeepEqual(pe.Data, want) {
			t.Fatalf("%s: data = %#v, want %#v", name, pe.Data, want)
		}
	}
	httpErr := func(body string) error {
		return &internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)}
	}
	_ = httpErr
	m := &LanguageModel{provider: New(Config{Name: "lmstudio", BaseURL: "http://127.0.0.1:1"})}
	check("match", m.handleError(httpErr(`{"error":{"message":"m","code":"c","x":1}}`)), map[string]interface{}{"error": map[string]interface{}{"message": "m", "code": "c"}})
	check("off schema", m.handleError(httpErr(`{"message":"m"}`)), nil)
	var pe *providererrors.ProviderError
	if errors.As(m.handleError(httpErr(`{"message":"m"}`)), &pe); pe.Message != "Bad Request" {
		t.Fatalf("off-schema message = %q, want the status text", pe.Message)
	}
}

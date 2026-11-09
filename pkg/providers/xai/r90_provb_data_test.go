package xai

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
	check("api", newXAIProviderError("xai", 400, nil, []byte(`{"error":{"message":"m","code":2,"x":1},"y":2}`)), map[string]interface{}{"error": map[string]interface{}{"message": "m", "code": float64(2)}})
	check("responses", newXAIProviderError("xai", 400, nil, []byte(`{"code":"c","error":"e","x":1}`)), map[string]interface{}{"code": "c", "error": "e"})
	check("off schema", newXAIProviderError("xai", 400, nil, []byte(`{"detail":"d"}`)), nil)
}

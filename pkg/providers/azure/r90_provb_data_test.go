package azure

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
	check("transcription error", azureSpeechAPIError("azure.transcription", 400, nil, []byte(`{"error":{"message":"m","code":"x"}}`), false), map[string]interface{}{"error": map[string]interface{}{"message": "m"}})
	check("transcription message", azureSpeechAPIError("azure.transcription", 400, nil, []byte(`{"message":"m","x":1}`), false), map[string]interface{}{"message": "m"})
	check("transcription off schema", azureSpeechAPIError("azure.transcription", 400, nil, []byte(`{"detail":"d"}`), false), nil)
	check("mai image", azureMaiImageAPIError(400, nil, []byte(`{"error":{"message":"m","code":1,"x":2}}`)), map[string]interface{}{"error": map[string]interface{}{"message": "m", "code": float64(1)}})
	check("mai image off schema", azureMaiImageAPIError(400, nil, []byte(`{"message":"m"}`)), nil)
}

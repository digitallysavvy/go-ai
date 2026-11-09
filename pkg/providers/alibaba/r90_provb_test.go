package alibaba

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
	check("chat", chatStatusError(400, nil, []byte(`{"error":{"message":"m","code":"c","y":1}}`)), map[string]interface{}{"error": map[string]interface{}{"message": "m", "code": "c"}})
	check("chat off schema", chatStatusError(400, nil, []byte(`{"message":"m"}`)), nil)
	check("native", alibabaNativeStatusError("alibaba", 400, []byte(`{"message":"m","request_id":"r","y":1}`), httpErr("")), map[string]interface{}{"message": "m", "request_id": "r"})
	check("native off schema", alibabaNativeStatusError("alibaba", 400, []byte(`{"error":"m"}`), httpErr("")), nil)
}

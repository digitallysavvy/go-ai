package providerutils

import (
	"errors"
	"reflect"
	"testing"

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
		return r90provbStatusErr(body)
	}
	_ = httpErr
	pe, _ := OpenAICompatibleAPIError("p", httpErr(`{"error":{"message":"m","type":"t","x":1},"request_id":"r"}`))
	check("match", pe, map[string]interface{}{"error": map[string]interface{}{"message": "m", "type": "t"}})
	pe, _ = OpenAICompatibleAPIError("p", httpErr(`{"message":"m"}`))
	check("off schema", pe, nil)
}

type r90provbStatusErr string

func (e r90provbStatusErr) Error() string { return "status 400" }

func (e r90provbStatusErr) HTTPStatusDetails() (int, map[string]string, []byte) {
	return 400, nil, []byte(e)
}

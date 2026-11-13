package fishaudio

import (
	"errors"
	"reflect"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// provc r90 V061 class (error data = TS schema output): fishAudioErrorDataSchema
// ({status?, message?}) output for a JSON object body, undefined otherwise.
func TestR90integErrorDataIsSchemaOutput(t *testing.T) {
	var pe *providererrors.ProviderError
	err := handleError(&internalhttp.HTTPStatusError{StatusCode: 402, Body: []byte(`{"status":402,"message":"pay","other":1}`)})
	if !errors.As(err, &pe) || pe.Message != "pay" || !reflect.DeepEqual(pe.Data, map[string]interface{}{"status": float64(402), "message": "pay"}) {
		t.Fatalf("schema body: %#v", err)
	}
	err = handleError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(`not json`)})
	if !errors.As(err, &pe) || pe.Message != "Bad Request" || pe.Data != nil {
		t.Fatalf("off-schema body: %#v", err)
	}
}

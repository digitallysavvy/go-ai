package gladia

import (
	"errors"
	"reflect"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR90provcErrorDataIsSchemaOutput: TS APICallError.data for the upload
// and init requests is the zod output of gladiaErrorDataSchema ({error:
// {message, code}}, undeclared keys stripped) for a matching body and
// undefined otherwise (the poll path already did this, r89).
func TestR90provcErrorDataIsSchemaOutput(t *testing.T) {
	data := func(body string) interface{} {
		var perr *providererrors.ProviderError
		if !errors.As(handleError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)}), &perr) {
			t.Fatalf("not a ProviderError for %s", body)
		}
		return perr.Data
	}
	want := map[string]interface{}{"error": map[string]interface{}{"message": "m", "code": float64(400)}}
	if got := data(`{"error":{"message":"m","code":400,"x":1},"request_id":"r"}`); !reflect.DeepEqual(got, want) {
		t.Errorf("matched data = %#v, want %#v", got, want)
	}
	if got := data(`{"message":"m"}`); got != nil {
		t.Errorf("off-schema data = %#v, want nil", got)
	}
}

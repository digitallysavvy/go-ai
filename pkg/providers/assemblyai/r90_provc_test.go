package assemblyai

import (
	"errors"
	"reflect"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR90provcErrorDataIsSchemaOutput: TS APICallError.data is the zod
// output of assemblyaiErrorDataSchema (undeclared keys stripped) for a
// matching body and undefined otherwise.
func TestR90provcErrorDataIsSchemaOutput(t *testing.T) {
	m := &TranscriptionModel{}
	data := func(body string) interface{} {
		var perr *providererrors.ProviderError
		if !errors.As(m.handleError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)}), &perr) {
			t.Fatalf("not a ProviderError for %s", body)
		}
		return perr.Data
	}
	if got := data(`{"error":"bad","status":"error"}`); !reflect.DeepEqual(got, map[string]interface{}{"error": "bad"}) {
		t.Errorf("flat data = %#v", got)
	}
	want := map[string]interface{}{"error": map[string]interface{}{"message": "m", "code": float64(7)}}
	if got := data(`{"error":{"message":"m","code":7,"x":1}}`); !reflect.DeepEqual(got, want) {
		t.Errorf("nested data = %#v, want %#v", got, want)
	}
	if got := data(`{"message":"m"}`); got != nil {
		t.Errorf("off-schema data = %#v, want nil", got)
	}
}

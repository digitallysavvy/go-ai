package deepgram

import (
	"errors"
	"reflect"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR90provcErrorDataIsSchemaOutput: TS APICallError.data is the zod
// output of deepgramErrorDataSchema ({err_code, err_msg, request_id?},
// undeclared keys stripped) for a matching body and undefined otherwise.
// The v0.5.2 Data["requestId"] key is kept alongside (S6).
func TestR90provcErrorDataIsSchemaOutput(t *testing.T) {
	data := func(body string) interface{} {
		var perr *providererrors.ProviderError
		if !errors.As(handleError("deepgram", &internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)}), &perr) {
			t.Fatalf("not a ProviderError for %s", body)
		}
		return perr.Data
	}
	want := map[string]interface{}{"err_code": "BAD", "err_msg": "m", "request_id": "r", "requestId": "r"}
	if got := data(`{"err_code":"BAD","err_msg":"m","request_id":"r","category":"x"}`); !reflect.DeepEqual(got, want) {
		t.Errorf("matched data = %#v, want %#v", got, want)
	}
	if got := data(`{"err_code":"BAD","err_msg":"m"}`); !reflect.DeepEqual(got, map[string]interface{}{"err_code": "BAD", "err_msg": "m"}) {
		t.Errorf("matched data without request_id = %#v", got)
	}
	if got := data(`{"error":"x"}`); got != nil {
		t.Errorf("off-schema data = %#v, want nil", got)
	}
}

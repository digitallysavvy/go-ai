package luma

import (
	"errors"
	"reflect"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR90provcErrorDataIsSchemaOutput: TS APICallError.data is the zod
// output of lumaErrorSchema (undeclared keys stripped) for a matching body
// and undefined otherwise.
func TestR90provcErrorDataIsSchemaOutput(t *testing.T) {
	m := &ImageModel{}
	data := func(body string) interface{} {
		var perr *providererrors.ProviderError
		if !errors.As(m.parseErrorResponseWithHeaders([]byte(body), 400, nil), &perr) {
			t.Fatalf("not a ProviderError for %s", body)
		}
		return perr.Data
	}
	want := map[string]interface{}{"detail": []interface{}{map[string]interface{}{"type": "t", "loc": []interface{}{"body"}, "msg": "bad", "input": "i", "ctx": map[string]interface{}{"expected": "e"}}}}
	if got := data(`{"detail":[{"type":"t","loc":["body"],"msg":"bad","input":"i","ctx":{"expected":"e","x":1},"url":"u"}],"extra":true}`); !reflect.DeepEqual(got, want) {
		t.Errorf("matched data = %#v, want %#v", got, want)
	}
	if got := data(`{"detail":"nope"}`); got != nil {
		t.Errorf("off-schema data = %#v, want nil", got)
	}
}

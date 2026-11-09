package prodia

import (
	"reflect"
	"testing"
)

// provc r90 V061 class (error data = TS schema output): APICallError.data is
// the TS error schema's output for a matching body (undeclared keys
// stripped, loose objects kept) and undefined for any other body.
func TestR90integErrorDataIsSchemaOutput(t *testing.T) {
	if got, want := prodiaErrorData([]byte(`{"foo":1,"detail":[1],"message":"m"}`)), interface{}(map[string]interface{}{"detail": []interface{}{float64(1)}, "message": "m"}); !reflect.DeepEqual(got, want) {
		t.Errorf("schema body: data = %#v, want %#v", got, want)
	}
	if got := prodiaErrorData([]byte(`{"error":{"message":"m"}}`)); got != nil {
		t.Errorf("off-schema body: data = %#v, want nil", got)
	}
}

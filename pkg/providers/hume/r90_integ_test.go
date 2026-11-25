package hume

import (
	"reflect"
	"testing"
)

// provc r90 V061 class (error data = TS schema output): APICallError.data is
// the TS error schema's output for a matching body (undeclared keys
// stripped, loose objects kept) and undefined for any other body.
func TestR90integErrorDataIsSchemaOutput(t *testing.T) {
	if got, want := humeErrorData([]byte(`{"error":{"message":"m","code":4,"x":1},"o":2}`)), interface{}(map[string]interface{}{"error": map[string]interface{}{"message": "m", "code": float64(4)}}); !reflect.DeepEqual(got, want) {
		t.Errorf("schema body: data = %#v, want %#v", got, want)
	}
	if got := humeErrorData([]byte(`{"error":{"message":"m"}}`)); got != nil {
		t.Errorf("off-schema body: data = %#v, want nil", got)
	}
}

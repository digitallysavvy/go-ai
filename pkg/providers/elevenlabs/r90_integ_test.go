package elevenlabs

import (
	"reflect"
	"testing"
)

// provc r90 V061 class (error data = TS schema output): APICallError.data is
// the TS error schema's output for a matching body (undeclared keys
// stripped, loose objects kept) and undefined for any other body.
func TestR90integErrorDataIsSchemaOutput(t *testing.T) {
	if got, want := elevenlabsErrorData([]byte(`{"detail":{"message":"m","extra":1},"error":{"message":"x","code":1}}`)), interface{}(map[string]interface{}{"detail": map[string]interface{}{"message": "m", "extra": float64(1)}}); !reflect.DeepEqual(got, want) {
		t.Errorf("schema body: data = %#v, want %#v", got, want)
	}
	if got := elevenlabsErrorData([]byte(`{"foo":1}`)); got != nil {
		t.Errorf("off-schema body: data = %#v, want nil", got)
	}
}

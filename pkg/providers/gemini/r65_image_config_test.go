package gemini

import (
	"reflect"
	"testing"
)

// TS google-image-model.ts / google-vertex-image-model.ts send imageConfig
// when `aspectRatio != null || userImageConfig != null`: an empty caller
// imageConfig is sent as {}; a missing or nil one with no aspect ratio is
// left out.
func TestR65ImageCallOptionsEmptyImageConfig(t *testing.T) {
	opts, _ := ImageCallOptions(map[string]interface{}{"imageConfig": map[string]interface{}{}}, "")
	if got, ok := opts["imageConfig"]; !ok || !reflect.DeepEqual(got, map[string]interface{}{}) {
		t.Fatalf("imageConfig = %#v (present %v), want {}", got, ok)
	}
	for _, user := range []map[string]interface{}{
		{},
		{"imageConfig": nil},
		{"imageConfig": map[string]interface{}(nil)},
	} {
		opts, _ := ImageCallOptions(user, "")
		if got, ok := opts["imageConfig"]; ok {
			t.Fatalf("user %#v: imageConfig = %#v, want absent", user, got)
		}
	}
}

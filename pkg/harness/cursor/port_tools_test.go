package cursor

import (
	"reflect"
	"testing"
)

// TS 6f9d73fd19 cursor-harness.test.ts.
func TestPortToolsBuildConfigWriteProtectedPaths(t *testing.T) {
	if got := BuildConfig(Settings{}).WriteProtectedPaths; !reflect.DeepEqual(got, []string{".cursor"}) {
		t.Fatalf("WriteProtectedPaths = %v", got)
	}
}

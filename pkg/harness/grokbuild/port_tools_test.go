package grokbuild

import (
	"reflect"
	"testing"
)

// TS 6f9d73fd19 grok-build-harness.test.ts.
func TestPortToolsBuildConfigWriteProtectedPaths(t *testing.T) {
	cfg, err := BuildConfig(Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.WriteProtectedPaths, []string{".grok"}) {
		t.Fatalf("WriteProtectedPaths = %v", cfg.WriteProtectedPaths)
	}
}

package libfxtest

import "testing"

func TestCurrentMode(t *testing.T) {
	for _, tc := range []struct {
		wasm, ci string
		want     Mode
	}{
		{"", "", ModeCachedOnly},
		{"1", "", ModeRequired},
		{"0", "", ModeSkip},
		{"", "true", ModeRequired},
		{"0", "true", ModeSkip},
	} {
		t.Setenv(EnvWasmTest, tc.wasm)
		t.Setenv("CI", tc.ci)
		if got := CurrentMode(); got != tc.want {
			t.Errorf("%s=%q CI=%q: mode %d, want %d", EnvWasmTest, tc.wasm, tc.ci, got, tc.want)
		}
	}
}

package fileutil

import "testing"

// TS new URL(a).origin treats the scheme's default port as absent for every
// special scheme (http/https/ws/wss/ftp), so an explicit default port is the
// same origin.
func TestIsSameOriginDefaultPortsAllSpecialSchemes(t *testing.T) {
	for _, c := range [][2]string{
		{"ws://h/x", "ws://h:80/y"},
		{"wss://h/x", "wss://h:443/y"},
		{"ftp://h/x", "ftp://h:21/y"},
	} {
		if !IsSameOrigin(c[0], c[1]) {
			t.Errorf("IsSameOrigin(%q,%q) = false, want true", c[0], c[1])
		}
	}
	if IsSameOrigin("ws://h/x", "wss://h/x") {
		t.Error("different schemes must differ")
	}
}

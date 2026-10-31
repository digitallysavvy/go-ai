package providerutils

import "testing"

func TestMediaTypeToExtensionSecondSegmentOnly(t *testing.T) {
	if got := MediaTypeToExtension("Audio/MPEG/extra"); got != "mp3" {
		t.Fatalf("got %q", got)
	}
	if got := MediaTypeToExtension("audio/ogg/x"); got != "ogg" {
		t.Fatalf("got %q", got)
	}
}

func TestIsSameOriginDefaultPortsAllSpecialSchemes(t *testing.T) {
	for _, c := range [][2]string{
		{"ws://h/x", "ws://h:80"},
		{"wss://h/x", "wss://h:443"},
		{"ftp://h/x", "ftp://h:21"},
	} {
		if !IsSameOrigin(c[0], c[1]) {
			t.Errorf("IsSameOrigin(%q,%q) = false, want true", c[0], c[1])
		}
	}
}

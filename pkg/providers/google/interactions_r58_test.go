package google

import "testing"

// r58: TS getTopLevelMediaType takes the text before the first "/" (or the
// whole string), so only an exact top-level type selects a block kind; a type
// that merely starts with "image"/"text" is unsupported.
func TestR58InteractionsMediaKindUsesTopLevelType(t *testing.T) {
	cases := map[string]string{
		"image/png":         "image",
		"image":             "image",
		"audio/wav":         "audio",
		"video/mp4":         "video",
		"application/pdf":   "document",
		"text/plain":        "document",
		"images/png":        "",
		"textual/x":         "",
		"applications/json": "",
		"videos":            "",
		"":                  "",
	}
	for mediaType, want := range cases {
		if got := interactionMediaKind(mediaType); got != want {
			t.Errorf("interactionMediaKind(%q) = %q, want %q", mediaType, got, want)
		}
	}
}

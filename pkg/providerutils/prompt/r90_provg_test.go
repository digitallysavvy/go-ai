package prompt

import (
	"strings"
	"testing"
)

// TestR90provgResolveFullMediaTypeQuoting: TS resolveFullMediaType writes
// `file of media type "${part.mediaType}" ...` (resolve-full-media-type.ts
// 42 and 47), so the media type is quoted as is, not Go-escaped.
func TestR90provgResolveFullMediaTypeQuoting(t *testing.T) {
	_, err := googleResolveFullMediaType(`image\x`, nil, false)
	if err == nil || !strings.Contains(err.Error(), `file of media type "image\x" must specify subtype since it is not passed as inline bytes`) {
		t.Fatalf("err = %v", err)
	}
	_, err = googleResolveFullMediaType(`image\x`, []byte{0}, true)
	if err == nil || !strings.Contains(err.Error(), `file of media type "image\x" must specify subtype since it could not be auto-detected`) {
		t.Fatalf("err = %v", err)
	}
}

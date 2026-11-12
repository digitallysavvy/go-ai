package heygen

import "testing"

// Typed-option ruling probe: a Go-typed heygen namespace (map[string]string)
// is read in its JSON form.
func TestR91provgTypedOptionLiterals(t *testing.T) {
	opts, err := parseVideoModelOptions(map[string]interface{}{"heygen": map[string]string{"mode": "text_to_video", "resolution": "1080p"}})
	if err != nil || opts == nil || opts.Mode != "text_to_video" || opts.Resolution != "1080p" {
		t.Fatalf("options = %+v, %v", opts, err)
	}
	if _, err := parseVideoModelOptions(map[string]interface{}{"heygen": "x"}); err == nil {
		t.Fatal("string namespace accepted")
	}
}

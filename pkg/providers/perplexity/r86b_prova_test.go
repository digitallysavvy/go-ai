package perplexity

import (
	"testing"
)

// TS withoutTrailingSlash(options.baseURL ?? default) removes one "/".
func TestR86bprovaBaseURLOneTrailingSlash(t *testing.T) {
	if got := New(Config{APIKey: "k", BaseURL: "https://x.example/api//"}).config.BaseURL; got != "https://x.example/api/" {
		t.Fatalf("BaseURL = %q", got)
	}
}

// TS /^data:image\/(png|jpeg|webp);base64,/i does not match U+017F for s
// (Node: false); BASE64 still matches.
func TestR86bprovaDecisionImageDataURLCaseFold(t *testing.T) {
	image := func(url string) map[string]interface{} {
		return map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": url}}
	}
	if _, err := countDecisionImages(image("data:image/png;ba\u017fe64,AAAA")); err == nil {
		t.Fatal("U+017F accepted")
	}
	if n, err := countDecisionImages(image("DATA:IMAGE/PNG;BASE64,AAAA")); err != nil || n != 1 {
		t.Fatalf("upper case: %d %v", n, err)
	}
}

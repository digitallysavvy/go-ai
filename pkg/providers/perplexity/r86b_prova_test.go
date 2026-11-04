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

package google

import "testing"

func TestNewTrimsOnlyOneTrailingSlash(t *testing.T) {
	p := New(Config{BaseURL: "https://proxy.test/v1beta//", APIKey: "k"})
	if p.config.BaseURL != "https://proxy.test/v1beta/" {
		t.Fatalf("BaseURL = %q", p.config.BaseURL)
	}
}

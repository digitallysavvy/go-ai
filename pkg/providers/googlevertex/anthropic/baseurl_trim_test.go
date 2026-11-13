package anthropic

import "testing"

// TS withoutTrailingSlash removes exactly one trailing slash.
func TestBaseURLTrimsOnlyOneTrailingSlash(t *testing.T) {
	p := New(Options{BaseURL: "https://example.test/v1//"})
	got, err := p.baseURL()
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.test/v1/" {
		t.Fatalf("baseURL = %q, want one slash trimmed", got)
	}
}

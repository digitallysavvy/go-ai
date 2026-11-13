package anthropic

import "testing"

// TS withoutTrailingSlash removes exactly one trailing slash.
func TestNormalizeBaseURLTrimsOnlyOneTrailingSlash(t *testing.T) {
	got, err := NormalizeBaseURL("https://proxy.test/v1//")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://proxy.test/v1/" {
		t.Fatalf("got %q", got)
	}
}

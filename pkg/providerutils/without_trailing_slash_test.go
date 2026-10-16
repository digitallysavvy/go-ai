package providerutils

import "testing"

func TestWithoutTrailingSlash(t *testing.T) {
	for in, want := range map[string]string{
		"":                       "",
		"https://x.test/v1":      "https://x.test/v1",
		"https://x.test/v1/":     "https://x.test/v1",
		"https://x.test/v1//":    "https://x.test/v1/",
		"/":                      "",
	} {
		if got := WithoutTrailingSlash(in); got != want {
			t.Errorf("WithoutTrailingSlash(%q) = %q, want %q", in, got, want)
		}
	}
}

package together

import "testing"

func TestTrimBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://h":       "https://h",
		"https://h/":      "https://h",
		"https://h/v1":    "https://h",
		"https://h/v1/":   "https://h",
		"https://h/x/v1":  "https://h/x",
		"https://h/other": "https://h/other",
	} {
		if got := trimBaseURL(in); got != want {
			t.Errorf("trimBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

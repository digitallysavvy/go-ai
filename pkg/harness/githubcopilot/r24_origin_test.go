package githubcopilot

import "testing"

func TestR24NormalizeGitHubOriginAndHost(t *testing.T) {
	cases := []struct{ in, origin, host string }{
		{"GitHub.COM", "https://github.com", "github.com"},
		{"https://Ghe.Example:443", "https://ghe.example", "ghe.example"},
		{"https://ghe.example:8443", "https://ghe.example:8443", "ghe.example"},
		{"http://ghe.example:80", "http://ghe.example", "ghe.example"},
		{"ssh://ghe.example", "null", "ghe.example"},
		{"https://[::1]:3000", "https://[::1]:3000", "[::1]"},
	}
	for _, c := range cases {
		if got := normalizeGitHubOrigin(c.in); got != c.origin {
			t.Errorf("origin(%q) = %q, want %q", c.in, got, c.origin)
		}
		if got := normalizeGitHubHost(c.in); got != c.host {
			t.Errorf("host(%q) = %q, want %q", c.in, got, c.host)
		}
	}
}

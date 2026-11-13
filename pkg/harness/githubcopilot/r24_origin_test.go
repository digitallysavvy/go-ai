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

// Node 24 `new URL(...)`.origin / .hostname (TS normalizeGitHubOrigin /
// normalizeGitHubHost).
func TestR87toolsNormalizeGitHubHostWHATWG(t *testing.T) {
	cases := []struct{ in, origin, host string }{
		{"GitHub.Example.com:443", "https://github.example.com", "github.example.com"},
		{"https://bücher.de", "https://xn--bcher-kva.de", "xn--bcher-kva.de"},
		{"https://x.com:8443/a", "https://x.com:8443", "x.com"},
		{"foo://Bar:1", "null", "Bar"},
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

package quiverai

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Expected hrefs come from Node's `new URL(input).toString()`; "" means the
// WHATWG parser throws.
func TestValidateQuiverAIImageURLMatchesNodeHref(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"https://example.com/a b?x=1 2#f g", "https://example.com/a%20b?x=1%202#f%20g"},
		{"https://example.com/a/../b/./c", "https://example.com/b/c"},
		{"https://example.com\\a\\b", "https://example.com/a/b"},
		{"http:example.com/x", "http://example.com/x"},
		{"https://exa\tmple.com/p", "https://example.com/p"},
		{"  https://example.com/  ", "https://example.com/"},
		{"https://example.com/é?q=é#é", "https://example.com/%C3%A9?q=%C3%A9#%C3%A9"},
		{"HTTP://EXAMPLE.COM:80/%7efoo", "http://example.com/%7efoo"},
		{"https://example.com:0443/", "https://example.com/"},
		{"https://user:pa ss@example.com/", "https://user:pa%20ss@example.com/"},
		{"https://example.com/?a=\"b'<>", "https://example.com/?a=%22b%27%3C%3E"},
		{"https://example.com/#", "https://example.com/#"},
		{"https://[0:0:0:0:0:0:0:1]/", "https://[::1]/"},
		{"https://0x7f.1/", "https://127.0.0.1/"},
		{"https://münchen.de/", "https://xn--mnchen-3ya.de/"},
		{"https://example.com/a%zz", "https://example.com/a%zz"},
		{"https:///example.com/", "https://example.com/"},
		{"https://example.com/%2e%2E/x", "https://example.com/x"},
		{"https://example.com:99999/", ""},
		{"https://ex%61mple.com/", "https://example.com/"},
		{"https://example.com/a?b#c#d", "https://example.com/a?b#c#d"},
		{"https://example.com/{x}|^`", "https://example.com/%7Bx%7D|%5E%60"},
		{"https://", ""},
		{"https://[::1", ""},
		{"https://1.2.3.256/", ""},
	} {
		got, err := validateQuiverAIImageURL(c.in, "x")
		if c.want == "" {
			if err == nil {
				t.Errorf("%q: got %q, want an error", c.in, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q: got %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

// Non-HTTP schemes parse in JS and fail the protocol check; schemeless text
// fails the parse.
func TestAnimationSourceURLMessagesFollowParseThenProtocol(t *testing.T) {
	for in, want := range map[string]string{
		"mailto:x@y.z": "requires an HTTP or HTTPS SVG URL.",
		"not a url":    "requires a valid HTTP or HTTPS SVG URL.",
		"https://":     "requires a valid HTTP or HTTPS SVG URL.",
	} {
		_, err := toQuiverAIAnimationSource(imageFileURL(in))
		if err == nil || !contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", in, err, want)
		}
	}
}

func imageFileURL(u string) provider.ImageFile { return provider.ImageFile{Type: "url", URL: u} }

func contains(s, sub string) bool { return strings.Contains(s, sub) }

package toolwebfetch

import (
	"net/http"
	"testing"
)

// Expected values are TS (Node 24) `new URL(raw)` with `hash = ”`, `.href`.
func TestCanonicalURLMatchesWHATWG(t *testing.T) {
	for raw, want := range map[string]string{
		"https://x.com/?q=é":                "https://x.com/?q=%C3%A9",
		"https://x.com/é":                   "https://x.com/%C3%A9",
		"https://x.com/a{b}c":               "https://x.com/a%7Bb%7Dc",
		"https://x.com/?a={b}|^":            "https://x.com/?a={b}|^",
		"https://x.com/./a/../b":            "https://x.com/b",
		"https://x.com/a/%2e%2e/b":          "https://x.com/b",
		"https://x.com/a/./b/%2E/c/.%2e/d":  "https://x.com/a/b/d",
		"https://x.com/..":                  "https://x.com/",
		"https://x.com/a/..?q":              "https://x.com/?q",
		"https://x.com/a/.":                 "https://x.com/a/",
		"https://x.com/a%2E":                "https://x.com/a%2E",
		"https://x.com/a/b/../../../c":      "https://x.com/c",
		"https://x.com:0443/":               "https://x.com/",
		"https://x.com:080/":                "https://x.com:80/",
		"http://x.com:/":                    "http://x.com/",
		"http://0x7f.0.0.1/":                "http://127.0.0.1/",
		"http://127.1/":                     "http://127.0.0.1/",
		"http://1.foo/":                     "http://1.foo/",
		"https://[0:0::1]/":                 "https://[::1]/",
		"https://[::FFFF:127.0.0.1]:8443/x": "https://[::ffff:7f00:1]:8443/x",
		"https://x.com/?q=%zz é'":           "https://x.com/?q=%zz%20%C3%A9%27",
		"http:x.com/a":                      "http://x.com/a",
		"http:///x.com/a":                   "http://x.com/a",
		"https:\\\\x.com\\a\\b?c\\d#e":      "https://x.com/a/b?c\\d",
		" https://x.com/a ":                 "https://x.com/a",
		"https://x.com/😀?😀":                 "https://x.com/%F0%9F%98%80?%F0%9F%98%80",
		"https://x.com?":                    "https://x.com/?",
	} {
		got, ok := canonicalURL(raw)
		if !ok || got.String() != want {
			t.Errorf("canonicalURL(%q) = %v, %v; want %q", raw, got, ok, want)
		}
	}
	// TS new URL throws for these.
	for _, raw := range []string{"http://foo.1/", "http://foo.0x/", "http://09.1/", "http://1.2.3.4.5/", "http://x.com:65536/"} {
		if got, ok := canonicalURL(raw); ok {
			t.Errorf("canonicalURL(%q) = %v, want invalid", raw, got)
		}
	}
}

// Expected values are TS `new URL(location, "http://a.com/b/c?d").toString()`.
func TestRedirectLocationsResolveLikeWHATWG(t *testing.T) {
	base, _ := canonicalURL("http://a.com/b/c?d")
	for location, want := range map[string]string{
		"/foo":                                "http://a.com/foo",
		"foo":                                 "http://a.com/b/foo",
		"../x/./y":                            "http://a.com/x/y",
		"//evil.com/a":                        "http://evil.com/a",
		"\\\\evil.com\\a":                     "http://evil.com/a",
		"/\\evil.com":                         "http://evil.com/",
		"http:foo":                            "http://a.com/b/foo",
		"http:/foo":                           "http://a.com/foo",
		"https:foo":                           "https://foo/",
		"http://other.com/a/../b":             "http://other.com/b",
		"HTTP://Other.com:80/":                "http://other.com/",
		"?q=é":                                "http://a.com/b/c?q=%C3%A9",
		"#frag":                               "http://a.com/b/c?d#frag",
		"/path?x=1#y":                         "http://a.com/path?x=1#y",
		" /trim ":                             "http://a.com/trim",
		"http://127.1/":                       "http://127.0.0.1/",
		"http://%6c%6f%63%61%6c%68%6f%73%74/": "http://localhost/",
	} {
		got, ok := resolveRedirect(base, location)
		if !ok || got.String() != want {
			t.Errorf("resolveRedirect(%q) = %v, %v; want %q", location, got, ok, want)
		}
	}
}

func TestFetchesTheCanonicalURL(t *testing.T) {
	var paths []string
	ft := serve(func(req *http.Request) *http.Response {
		paths = append(paths, req.URL.RequestURI())
		if req.URL.Path == "/old" {
			return response(302, "", "Location", "./x/../new#top")
		}
		return response(200, "ok", "Content-Type", "text/plain")
	})
	tool := newTool(t, Options{Transport: ft})
	d := doc(t, run(t, tool, "https://example.com/a/../b?q=é", 0))
	if d.FinalURL != "https://example.com/b?q=%C3%A9" {
		t.Fatalf("final = %q", d.FinalURL)
	}
	d = doc(t, run(t, tool, "https://example.com/old", 0))
	if d.FinalURL != "https://example.com/new#top" {
		t.Fatalf("final = %q", d.FinalURL)
	}
	if want := []string{"/b?q=%C3%A9", "/old", "/new"}; len(paths) != 3 || paths[0] != want[0] || paths[1] != want[1] || paths[2] != want[2] {
		t.Fatalf("paths = %q, want %q", paths, want)
	}
	// A stray "%" is fetched rather than rejected as an invalid URL.
	doc(t, run(t, tool, "https://example.com/100%", 0))
}

func TestUserMessageMatchUsesWHATWGNormalization(t *testing.T) {
	ft := fixed(200, "ok", "Content-Type", "text/plain")
	tool := newTool(t, Options{Transport: ft, RequireURLInUserMessage: true})
	// The tool trims the requested URL like new URL does.
	doc(t, run(t, tool, " https://example.com/docs", 0, userMessage("Read https://example.com/docs")))
	// Encoded and unencoded query and path forms are the same URL.
	doc(t, run(t, tool, "https://example.com/search?q=%C3%A9", 0, userMessage("Search https://example.com/search?q=é")))
	doc(t, run(t, tool, "https://example.com/b", 0, userMessage("Read https://example.com/a/../b")))
	doc(t, run(t, tool, "http://8.8.8.8:8080/", 0, userMessage("see http://134744072:08080/")))
}

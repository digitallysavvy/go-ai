package urlnorm

import "testing"

// Expected values from Node 24: `x = new URL(u); x.protocol = p; x.href`,
// `x.pathname = p`, and `new URL(p, base).href` (review round 94; used by
// the WebSocket URL builders that port TS toWebSocketUrl / new URL).
func TestR94integSetProtocol(t *testing.T) {
	for _, tc := range [][3]string{
		{"https://API.Example.com:443/v1", "wss:", "wss://api.example.com/v1"},
		{"http://h:80/x", "ws:", "ws://h/x"},
		{"http://h:443/x", "wss:", "wss://h/x"},
		{"https://h:80/x", "ws:", "ws://h/x"},
		{"https://h/x", "ftp:", "ftp://h/x"},
		{"https://h/x", "foo:", "https://h/x"},
		{"foo://h/x", "wss:", "foo://h/x"},
		{"foo://h/x", "bar:", "bar://h/x"},
		{"http://u:p@h/x", "file:", "http://u:p@h/x"},
		{"wss://h/x", "ws:", "ws://h/x"},
		{"https://h/x", "1ws:", "https://h/x"},
		{"https://h/x", "w s:", "https://h/x"},
		{"https://h/x", "wss", "wss://h/x"},
	} {
		u, err := Parse(tc[0])
		if err != nil {
			t.Fatal(err)
		}
		u.SetProtocol(tc[1])
		if got := u.String(); got != tc[2] {
			t.Errorf("%s protocol=%q: %s, want %s", tc[0], tc[1], got, tc[2])
		}
	}
}

func TestR94integSetPathname(t *testing.T) {
	for _, tc := range [][3]string{
		{"https://h/a/b?q=1#f", "/c/d", "https://h/c/d?q=1#f"},
		{"https://h/a", "c", "https://h/c"},
		{"https://h/a", "/x?y#z", "https://h/x%3Fy%23z"},
		{"https://h/a", "/x y\t\n/z ", "https://h/x%20y/z%20"},
		{"https://h/a", "/a/../b/./c", "https://h/b/c"},
		{"https://h/a", `\x\y`, "https://h/x/y"},
		{"https://h/a", "", "https://h/"},
		{"https://h/a", "//x", "https://h//x"},
		{"foo://h/a", "x", "foo://h/x"},
		{"foo:/a", "//x", "foo:/.//x"},
		{"https://h/a", "/%41%zz", "https://h/%41%zz"},
		{"https://h/a", "/é", "https://h/%C3%A9"},
		{"mailto:x", "/y", "mailto:x"},
	} {
		u, err := Parse(tc[0])
		if err != nil {
			t.Fatal(err)
		}
		u.SetPathname(tc[1])
		if got := u.String(); got != tc[2] {
			t.Errorf("%s pathname=%q: %s, want %s", tc[0], tc[1], got, tc[2])
		}
	}
}

func TestR94integResolvePath(t *testing.T) {
	for _, tc := range [][3]string{
		{"https://User:Pw@API.Cartesia.AI:443/v1/x?y=1#z", "/stt/websocket", "https://User:Pw@api.cartesia.ai/stt/websocket"},
		{"http://h:8080/base/", "/stt/turns/websocket", "http://h:8080/stt/turns/websocket"},
		{"https://h", "/a/../b", "https://h/b"},
		{"foo://h/x", "/y", "foo://h/y"},
	} {
		base, err := Parse(tc[0])
		if err != nil {
			t.Fatal(err)
		}
		u, err := base.ResolvePath(tc[1])
		if err != nil {
			t.Fatalf("%s + %s: %v", tc[0], tc[1], err)
		}
		if got := u.String(); got != tc[2] {
			t.Errorf("new URL(%q, %q) = %s, want %s", tc[1], tc[0], got, tc[2])
		}
	}
	base, _ := Parse("mailto:x")
	if _, err := base.ResolvePath("/y"); err != ErrInvalidURL {
		t.Errorf("opaque base: err = %v, want Invalid URL", err)
	}
}

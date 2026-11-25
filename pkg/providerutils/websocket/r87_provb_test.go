package websocket

import "testing"

// The handshake Origin header is the target's origin: scheme and host only,
// never the URL's userinfo, path or query.
func TestR87provbDeriveOriginDropsUserinfo(t *testing.T) {
	got, err := deriveOrigin("wss://user:secret@api.example.com:8443/v1/stream?token=x#f")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://api.example.com:8443/"; got != want {
		t.Fatalf("origin = %q, want %q", got, want)
	}
}

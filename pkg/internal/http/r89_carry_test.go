package http

import (
	"context"
	stdhttp "net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// CR r89 provb -> carry: fetch drops Authorization on any cross-origin
// redirect (scheme, host and port); net/http compares only the host, so a
// 127.0.0.1:A -> 127.0.0.1:B redirect delivered the Bearer key to port B.
func TestR89carryRedirectDropsAuthorizationAcrossOrigins(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]string{}
	record := func(name string, r *stdhttp.Request) {
		mu.Lock()
		seen[name] = r.Header.Get("Authorization") + "|" + r.Header.Get("X-Api-Key")
		mu.Unlock()
	}
	var origin *httptest.Server
	other := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		record("other"+r.URL.Path, r)
		if r.URL.Path == "/back" {
			stdhttp.Redirect(w, r, origin.URL+"/final", stdhttp.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer other.Close()
	origin = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		record("origin"+r.URL.Path, r)
		switch r.URL.Path {
		case "/same":
			stdhttp.Redirect(w, r, "/final", stdhttp.StatusFound)
		case "/cross":
			stdhttp.Redirect(w, r, other.URL+"/end", stdhttp.StatusFound)
		case "/roundtrip":
			stdhttp.Redirect(w, r, other.URL+"/back", stdhttp.StatusFound)
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer origin.Close()

	c := NewClient(Config{BaseURL: origin.URL, Headers: map[string]string{"Authorization": "Bearer k", "X-Api-Key": "custom"}})
	get := func(path string) {
		t.Helper()
		var out map[string]any
		if err := c.DoJSON(context.Background(), Request{Method: stdhttp.MethodGet, Path: path}, &out); err != nil {
			t.Fatal(err)
		}
	}
	get("/same")
	if seen["origin/final"] != "Bearer k|custom" {
		t.Fatalf("same-origin hop: %q", seen["origin/final"])
	}
	get("/cross")
	if seen["other/end"] != "|custom" {
		t.Fatalf("other-port hop got %q (custom headers stay, as fetch copies them)", seen["other/end"])
	}
	seen = map[string]string{}
	get("/roundtrip")
	if seen["other/back"] != "|custom" || seen["origin/final"] != "|custom" {
		t.Fatalf("roundtrip: %v", seen)
	}
	seen = map[string]string{}
	stream, err := c.DoStream(context.Background(), Request{Method: stdhttp.MethodGet, Path: "/cross"})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Body.Close()
	if seen["other/end"] != "|custom" {
		t.Fatalf("stream other-port hop got %q", seen["other/end"])
	}
	resp, err := withFetchRedirects(&stdhttp.Client{}).Get(origin.URL + "/same")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

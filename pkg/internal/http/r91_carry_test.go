package http

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// core3-09: TS postToApi / getFromApi treat anything but 2xx (response.ok)
// as a failure, including an unfollowed 3xx.
func TestR91carryUnfollowed3xxIsHTTPStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()
	srv302 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound) // no Location: not followed
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv302.Close()
	ctx := context.Background()
	for _, tc := range []struct {
		url  string
		code int
	}{{srv.URL, 304}, {srv302.URL, 302}} {
		c := NewClient(Config{BaseURL: tc.url})
		var out map[string]interface{}
		var hs *HTTPStatusError
		if err := c.DoJSON(ctx, Request{Method: http.MethodGet, Path: "/"}, &out); !errors.As(err, &hs) || hs.StatusCode != tc.code {
			t.Fatalf("DoJSON %d: err = %v", tc.code, err)
		}
		if _, err := c.DoJSONResponse(ctx, Request{Method: http.MethodGet, Path: "/"}, &out); !errors.As(err, &hs) || hs.StatusCode != tc.code {
			t.Fatalf("DoJSONResponse %d: err = %v", tc.code, err)
		}
		if _, err := c.DoStream(ctx, Request{Method: http.MethodGet, Path: "/"}); !errors.As(err, &hs) || hs.StatusCode != tc.code {
			t.Fatalf("DoStream %d: err = %v", tc.code, err)
		}
	}
}

// core3-08: undici httpRedirectFetch drops Authorization,
// Proxy-Authorization and Cookie on a cross-origin hop (same host, other
// port here, which net/http alone treats as same-host).
func TestR91carryCrossOriginRedirectDropsCredentialHeaders(t *testing.T) {
	var seen http.Header
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/next", http.StatusFound)
	}))
	defer origin.Close()
	if strings.Split(strings.TrimPrefix(origin.URL, "http://"), ":")[0] != strings.Split(strings.TrimPrefix(target.URL, "http://"), ":")[0] {
		t.Skip("servers on different hosts")
	}
	c := NewClient(Config{BaseURL: origin.URL, Headers: map[string]string{
		"Authorization":       "Bearer k",
		"Proxy-Authorization": "Basic x",
		"Cookie":              "s=1",
		"X-Keep":              "1",
	}})
	var out map[string]interface{}
	if err := c.DoJSON(context.Background(), Request{Method: http.MethodGet, Path: "/"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"Authorization", "Proxy-Authorization", "Cookie"} {
		if seen.Get(h) != "" {
			t.Fatalf("%s forwarded across origins: %q", h, seen.Get(h))
		}
	}
	if seen.Get("X-Keep") != "1" {
		t.Fatalf("other headers must be kept: %v", seen)
	}
}

// Same-origin redirects keep the credential headers (fetch only strips on
// an origin change).
func TestR91carrySameOriginRedirectKeepsCredentialHeaders(t *testing.T) {
	var seen http.Header
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, srv.URL+"/next", http.StatusFound)
			return
		}
		seen = r.Header.Clone()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, Headers: map[string]string{"Proxy-Authorization": "Basic x", "Cookie": "s=1"}})
	var out map[string]interface{}
	if err := c.DoJSON(context.Background(), Request{Method: http.MethodGet, Path: "/"}, &out); err != nil {
		t.Fatal(err)
	}
	if seen.Get("Proxy-Authorization") != "Basic x" || seen.Get("Cookie") != "s=1" {
		t.Fatalf("same-origin hop lost headers: %v", seen)
	}
}

// B1 behavior: a real dial failure through the client is the retryable
// "Cannot connect to API" error (TS handleFetchError).
func TestR91carryTransportErrorFromDialFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	c := NewClient(Config{BaseURL: "http://" + addr})
	var out map[string]interface{}
	err = c.DoJSON(context.Background(), Request{Method: http.MethodGet, Path: "/"}, &out)
	if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") {
		t.Fatalf("err = %v", err)
	}
	r, ok := err.(interface{ IsRetryable() bool })
	if !ok || !r.IsRetryable() {
		t.Fatalf("not retryable: %T", err)
	}
}

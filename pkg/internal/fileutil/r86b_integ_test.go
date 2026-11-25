package fileutil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// cr-r86b-tools-to-integrator 3: TS fetchWithValidatedRedirects resolves
// new URL(response.headers.get('location'), currentUrl). Node fetch reads a
// raw `Location: /caf\xc3\xa9` as "/cafÃ©" (requested as
// /caf%C3%83%C2%A9) and joins two Location lines `/a` + `/b` as "/a, /b"
// (/a,%20/b); the Content-Type is read the same way.
func TestR86bintegDownloadRedirectLocationReadsAsFetch(t *testing.T) {
	var mu sync.Mutex
	var got []string
	mux := http.NewServeMux()
	mux.HandleFunc("/start-utf8", func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Location"] = []string{"/caf\xc3\xa9"}
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/start-two", func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Location"] = []string{"/a", "/b"}
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/start-plain", func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Location"] = []string{"/plain"}
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.URL.EscapedPath())
		mu.Unlock()
		w.Header()["Content-Type"] = []string{"text/plain; charset=\xe9", "x"}
		_, _ = w.Write([]byte("ok"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	for _, tc := range []struct{ start, wantPath string }{
		{"/start-utf8", "/caf%C3%83%C2%A9"},
		{"/start-two", "/a,%20/b"},
		{"/start-plain", "/plain"},
	} {
		mu.Lock()
		got = nil
		mu.Unlock()
		res, err := DownloadWithMetadata(context.Background(), srv.URL+tc.start, DownloadOptions{})
		if err != nil {
			t.Fatalf("%s: %v", tc.start, err)
		}
		mu.Lock()
		if len(got) != 1 || got[0] != tc.wantPath {
			t.Errorf("%s: requested %v, want %s (TS)", tc.start, got, tc.wantPath)
		}
		mu.Unlock()
		if want := "text/plain; charset=é, x"; res.ContentType != want {
			t.Errorf("%s: ContentType %q, want %q", tc.start, res.ContentType, want)
		}
	}
}

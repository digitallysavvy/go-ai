package anthropic

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// r88provaRedirectServers starts a foreign server (reached as localhost) that
// records the headers it receives, and an API server (127.0.0.1) that sends
// every request there with a 307 (method and body kept). It returns the API
// base URL, a function returning the foreign server's request headers, and a
// function returning the API server's request headers.
func r88provaRedirectServers(t *testing.T) (string, func() []http.Header, func() []http.Header) {
	t.Helper()
	var mu sync.Mutex
	var foreign, api []http.Header
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		foreign = append(foreign, r.Header.Clone())
		mu.Unlock()
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(other.Close)
	target := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		api = append(api, r.Header.Clone())
		mu.Unlock()
		http.Redirect(w, r, target+"/elsewhere", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)
	get := func(h *[]http.Header) func() []http.Header {
		return func() []http.Header {
			mu.Lock()
			defer mu.Unlock()
			return append([]http.Header(nil), (*h)...)
		}
	}
	return srv.URL, get(&foreign), get(&api)
}

// r88provaCredentialHeaders lists the credential and custom headers a
// foreign origin must never receive.
var r88provaCredentialHeaders = []string{"Authorization", "X-Api-Key", "X-Custom-Header", "X-Amz-Date", "X-Amz-Security-Token", "X-Amz-Content-Sha256"}

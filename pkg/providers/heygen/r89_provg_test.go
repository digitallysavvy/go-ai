package heygen

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

const r89provgStatusBody = `{"data":{"status":"completed","video_url":"https://cdn.example/v.mp4"}}`

// TS heygen-video-model.ts 380-385: doStatus fetches with validateUrl: true,
// trustedOrigin and credentialedOrigin = baseURL (provider-utils
// get-from-api.ts / fetch-with-validated-redirects.ts): a redirect hop to
// another origin carries no caller headers (the x-api-key stays home) and is
// URL-validated (a loopback target is blocked), while a same-origin hop keeps
// the headers.
func TestR89provgHeyGenStatusRedirects(t *testing.T) {
	t.Run("cross-origin hop gets no api key and is validated", func(t *testing.T) {
		var mu sync.Mutex
		var foreignKeys []string
		foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			foreignKeys = append(foreignKeys, r.Header.Get("X-Api-Key"))
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, r89provgStatusBody)
		}))
		defer foreign.Close()
		home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, foreign.URL+"/elsewhere", http.StatusFound)
		}))
		defer home.Close()

		m := NewVideoModel(New(Config{APIKey: "secret-key", BaseURL: home.URL}), "heygen-video-1")
		_, err := m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
		if err == nil {
			t.Fatal("redirect to an unvalidated loopback origin succeeded, want a blocked download")
		}
		if !providererrors.IsDownloadError(err) {
			t.Fatalf("error = %T %v, want DownloadError (TS fetchWithValidatedRedirects)", err, err)
		}
		mu.Lock()
		defer mu.Unlock()
		for _, k := range foreignKeys {
			if k != "" {
				t.Fatalf("foreign origin received x-api-key %q", k)
			}
		}
	})

	t.Run("same-origin hop keeps the api key", func(t *testing.T) {
		var mu sync.Mutex
		var keys []string
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			keys = append(keys, r.Header.Get("X-Api-Key"))
			mu.Unlock()
			if r.URL.Path != "/moved" {
				http.Redirect(w, r, srv.URL+"/moved", http.StatusFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Request-Id", "r1")
			_, _ = io.WriteString(w, r89provgStatusBody)
		}))
		defer srv.Close()

		m := NewVideoModel(New(Config{APIKey: "secret-key", BaseURL: srv.URL}), "heygen-video-1")
		res, err := m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
		if err != nil {
			t.Fatal(err)
		}
		if res.Status != provider.VideoOperationStatusCompleted || len(res.Videos) != 1 || res.Videos[0].URL != "https://cdn.example/v.mp4" {
			t.Fatalf("result = %+v", res)
		}
		if res.Response.Headers["X-Request-Id"] != "r1" {
			t.Fatalf("response headers = %#v", res.Response.Headers)
		}
		mu.Lock()
		defer mu.Unlock()
		if !reflect.DeepEqual(keys, []string{"secret-key", "secret-key"}) {
			t.Fatalf("x-api-key per hop = %q", keys)
		}
	})

	t.Run("API error keeps the heygen failed response handler", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"code":"rate_limited","message":"Slow down","param":"video_id"}}`)
		}))
		defer srv.Close()
		m := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "heygen-video-1")
		_, err := m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: testOperation})
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) {
			t.Fatalf("error = %T %v, want ProviderError", err, err)
		}
		if pe.StatusCode != 429 || pe.Message != "Slow down (rate_limited) [video_id]" || pe.ErrorCode != "rate_limited" {
			t.Fatalf("error = %d %q %q", pe.StatusCode, pe.Message, pe.ErrorCode)
		}
		if pe.ResponseHeaders["Retry-After"] != "3" {
			t.Fatalf("response headers = %#v", pe.ResponseHeaders)
		}
	})
}

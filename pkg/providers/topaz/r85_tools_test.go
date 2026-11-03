package topaz

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestR85toolsUploadRejectsRedirect ports TS topaz-video-model.test.ts
// "rejects a %i upload redirect and cancels the request" (c66402d340): a
// 307/308 on the input-video PUT fails the start, the video body is never
// replayed to the redirect target, and the request is canceled.
func TestR85toolsUploadRejectsRedirect(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var mu sync.Mutex
			var calls []string
			var redirectedBody []byte
			record := func(r *http.Request) {
				mu.Lock()
				calls = append(calls, r.Method+" "+r.URL.Path)
				mu.Unlock()
			}
			mux := http.NewServeMux()
			var srv *httptest.Server
			mux.HandleFunc("/video/express", func(w http.ResponseWriter, r *http.Request) {
				record(r)
				_, _ = fmt.Fprintf(w, `{"requestId":%q,"uploadUrls":[%q]}`, videoRequestID, srv.URL+"/upload/part-1")
			})
			mux.HandleFunc("/upload/part-1", func(w http.ResponseWriter, r *http.Request) {
				record(r)
				_, _ = io.ReadAll(r.Body)
				w.Header().Set("Location", srv.URL+"/upload/redirected")
				w.WriteHeader(status)
			})
			mux.HandleFunc("/upload/redirected", func(w http.ResponseWriter, r *http.Request) {
				record(r)
				b, _ := io.ReadAll(r.Body)
				mu.Lock()
				redirectedBody = b
				mu.Unlock()
				w.WriteHeader(http.StatusNoContent)
			})
			mux.HandleFunc("/video/"+videoRequestID, func(w http.ResponseWriter, r *http.Request) {
				record(r)
				w.WriteHeader(http.StatusNoContent)
			})
			srv = httptest.NewServer(mux)
			defer srv.Close()

			prov := New(Config{APIKey: "test-key", BaseURL: srv.URL, VideoPollIntervalMillis: 1, VideoPollTimeoutMillis: 200})
			_, err := NewVideoModel(prov, VideoModelStarlightPrecise26).DoStart(context.Background(), defaultStartOptions())
			if err == nil {
				t.Fatal("DoStart succeeded; want the redirected upload to fail")
			}
			mu.Lock()
			defer mu.Unlock()
			want := []string{"POST /video/express", "PUT /upload/part-1", "DELETE /video/" + videoRequestID}
			if fmt.Sprint(calls) != fmt.Sprint(want) {
				t.Errorf("calls = %v, want %v", calls, want)
			}
			if redirectedBody != nil {
				t.Errorf("video body replayed to the redirect target: %v", redirectedBody)
			}
		})
	}
}

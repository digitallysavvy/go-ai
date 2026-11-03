package topaz

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS uploadVideo throws APICallError({responseBody: await response.text(),
// responseHeaders: Object.fromEntries(response.headers.entries())}): the
// body is WHATWG-decoded (BOM dropped, invalid UTF-8 replaced) and the
// headers are lower-cased.
func TestR86toolsUploadFailureBodyAndHeaders(t *testing.T) {
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/video/express", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"requestId":%q,"uploadUrls":[%q]}`, videoRequestID, srv.URL+"/upload/part-1")
	})
	mux.HandleFunc("/upload/part-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upload-Error", "denied")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("\xEF\xBB\xBFno\xFF"))
	})
	mux.HandleFunc("/video/"+videoRequestID, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()

	prov := New(Config{APIKey: "test-key", BaseURL: srv.URL, VideoPollIntervalMillis: 1, VideoPollTimeoutMillis: 200})
	_, err := NewVideoModel(prov, VideoModelStarlightPrecise26).DoStart(context.Background(), defaultStartOptions())
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("err = %v (%T), want ProviderError", err, err)
	}
	if perr.StatusCode != http.StatusForbidden || perr.ResponseBody != "no�" || perr.ResponseHeaders["x-upload-error"] != "denied" {
		t.Fatalf("got status %d body %q headers %v", perr.StatusCode, perr.ResponseBody, perr.ResponseHeaders)
	}
}

type r86toolsCtxKey struct{}

// prova CR r86 2 (V061 item 24 class): the same-origin download URL gets the
// provider headers resolved with the call's ctx, so an APIKeyFunc sees the
// caller's ctx values there too (TS resolves config.headers() in the call).
func TestR86toolsImageDownloadHeadersUseCallContext(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()
	var mu sync.Mutex
	var seen []interface{}
	prov := New(Config{BaseURL: ts.server.URL, APIKeyFunc: func(ctx context.Context) (string, error) {
		mu.Lock()
		seen = append(seen, ctx.Value(r86toolsCtxKey{}))
		mu.Unlock()
		return "func-key", nil
	}})
	ctx := context.WithValue(context.Background(), r86toolsCtxKey{}, "caller")
	if _, err := NewImageModel(prov, ImageModelWonder35).DoGenerate(ctx, defaultImageOptions(ts)); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for i, v := range seen {
		if v != "caller" {
			t.Fatalf("APIKeyFunc call %d saw ctx value %v, want the caller's", i, v)
		}
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if got := ts.dlHeader.Get("X-Api-Key"); got != "func-key" {
		t.Fatalf("download X-Api-Key = %q", got)
	}
}

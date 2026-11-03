package topaz

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

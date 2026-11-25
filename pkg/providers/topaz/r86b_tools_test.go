package topaz

import (
	"context"
	"net/http"
	"testing"
)

// r86btoolsRepeatHeader adds a header sent on two lines to every response.
func r86btoolsRepeatHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["X-Rep"] = []string{"a", "b"}
		next.ServeHTTP(w, r)
	})
}

// TS response headers (extractResponseHeaders / Object.fromEntries(
// response.headers.entries())) join a header's repeated lines with ", ";
// Go kept only the first value for the image download and the video start.
func TestR86btoolsResponseHeadersJoinRepeatedLines(t *testing.T) {
	its := newImageTestServer(t)
	defer its.server.Close()
	its.server.Config.Handler = r86btoolsRepeatHeader(its.server.Config.Handler)
	img, err := NewImageModel(New(Config{APIKey: "test-key", BaseURL: its.server.URL}), ImageModelWonder35).DoGenerate(context.Background(), defaultImageOptions(its))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Response.Headers["X-Rep"]; got != "a, b" {
		t.Errorf("image Response.Headers[X-Rep] = %q, want %q", got, "a, b")
	}

	vts := newVideoTestServer(t)
	defer vts.server.Close()
	vts.server.Config.Handler = r86btoolsRepeatHeader(vts.server.Config.Handler)
	prov := New(Config{APIKey: "test-key", BaseURL: vts.server.URL, VideoPollIntervalMillis: 1, VideoPollTimeoutMillis: 2000})
	start, err := NewVideoModel(prov, VideoModelStarlightPrecise26).DoStart(context.Background(), defaultStartOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got := start.Response.Headers["X-Rep"]; got != "a, b" {
		t.Errorf("video start Response.Headers[X-Rep] = %q, want %q", got, "a, b")
	}
}

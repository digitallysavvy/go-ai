package ai

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// TS createDefaultDownloadFunction downloads concurrently (Promise.all).
func TestCreateDownloadRunsRequestsConcurrently(t *testing.T) {
	restore := downloadURLValidator
	downloadURLValidator = func(string) error { return nil }
	t.Cleanup(func() { downloadURLValidator = restore })
	allowLoopbackDownloadTransport(t)

	var inflight, peak int32
	srv := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&inflight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
		atomic.AddInt32(&inflight, -1)
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()

	res, err := CreateDownload(nil)(context.Background(), []DownloadRequest{
		{URL: srv.URL + "/a"}, {URL: srv.URL + "/b"}, {URL: srv.URL + "/c", IsURLSupportedByModel: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res[0] == nil || res[1] == nil || res[2] != nil {
		t.Fatalf("results=%v", res)
	}
	if atomic.LoadInt32(&peak) < 2 {
		t.Fatalf("downloads were sequential, peak=%d", peak)
	}
}

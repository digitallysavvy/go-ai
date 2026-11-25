package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

type r85carryKey struct{}

// HeadersFuncCtx (the request-time credential hook for TS Resolvable API keys
// and auth tokens, 7716e189ee / 63bf860638) sees the request's context, is
// layered over HeadersFunc, and an error aborts the request before it is
// sent.
func TestR85carryHeadersFuncCtx(t *testing.T) {
	var hits int32
	var gotAuth, gotStatic string
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		atomic.AddInt32(&hits, 1)
		gotAuth, gotStatic = r.Header.Get("Authorization"), r.Header.Get("X-Static")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	fail := errors.New("no key")
	var failNow atomic.Bool
	c := NewClient(Config{
		BaseURL:     srv.URL,
		Headers:     map[string]string{"X-Static": "s"},
		HeadersFunc: func() (map[string]string, error) { return map[string]string{"Authorization": "Bearer static"}, nil },
		HeadersFuncCtx: func(ctx context.Context) (map[string]string, error) {
			if failNow.Load() {
				return nil, fail
			}
			return map[string]string{"Authorization": "Bearer " + ctx.Value(r85carryKey{}).(string)}, nil
		},
	})
	ctx := context.WithValue(context.Background(), r85carryKey{}, "per-request")
	if _, err := c.Do(ctx, Request{Method: "GET", Path: "/x"}); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer per-request" || gotStatic != "s" {
		t.Fatalf("Authorization = %q, X-Static = %q", gotAuth, gotStatic)
	}
	headers, err := c.RequestHeadersCtx(ctx)
	if err != nil || headers["Authorization"] != "Bearer per-request" {
		t.Fatalf("RequestHeadersCtx = %v, %v", headers, err)
	}
	failNow.Store(true)
	if _, err := c.Do(ctx, Request{Method: "GET", Path: "/x"}); !errors.Is(err, fail) {
		t.Fatalf("Do err = %v, want the hook's error", err)
	}
	if resp, err := c.DoStream(ctx, Request{Method: "GET", Path: "/x"}); !errors.Is(err, fail) {
		if resp != nil {
			_ = resp.Body.Close()
		}
		t.Fatalf("DoStream err = %v, want the hook's error", err)
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("server hits = %d, want 1 (failed hooks send nothing)", n)
	}
	if _, err := c.RequestHeaders(); !errors.Is(err, fail) {
		t.Fatalf("RequestHeaders err = %v", err)
	}
	if h := c.Headers(); h["Authorization"] != "" || h["X-Static"] != "s" {
		t.Fatalf("Headers() with a failing hook = %v, want the defaults", h)
	}
}

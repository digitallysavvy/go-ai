package topaz

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

type r86integCtxKey struct{}

// The best-effort cancel after a caller abort keeps the caller's context
// values without its cancellation, so an APIKeyFunc that reads them (TS
// resolves config.headers() in the call) still gets them; it got
// context.Background().
func TestR86integCancelAfterAbortKeyFuncSeesCallerValues(t *testing.T) {
	ts := newImageTestServer(t)
	defer ts.server.Close()
	var mu sync.Mutex
	var seen []interface{}
	keyFunc := func(ctx context.Context) (string, error) {
		mu.Lock()
		seen = append(seen, ctx.Value(r86integCtxKey{}))
		mu.Unlock()
		return "test-key", nil
	}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), r86integCtxKey{}, "caller"))
	ts.statusFn = func(count int) (int, string) {
		cancel()
		return http.StatusOK, `{"status":"Processing"}`
	}
	m := NewImageModel(New(Config{APIKeyFunc: keyFunc, BaseURL: ts.server.URL}), ImageModelWonder35)
	if _, err := m.DoGenerate(ctx, defaultImageOptions(ts)); err == nil {
		t.Fatal("expected an error after the caller aborts")
	}
	deadline := time.Now().Add(time.Second)
	for !ts.wasCancelCalled() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !ts.wasCancelCalled() {
		t.Fatal("the job was not cancelled")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, v := range seen {
		if v != "caller" {
			t.Fatalf("key func ctx values = %v, want the caller's on every request", seen)
		}
	}
}

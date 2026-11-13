package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TS getAvailableModels returns the stale cache immediately once the refresh
// window has elapsed and refreshes in the background.
func TestGetAvailableModels_ReturnsStaleWhileRefreshing(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		id := "a/v1"
		if n > 1 {
			id = "a/v2"
		}
		_, _ = w.Write([]byte(`{"models":[{"id":"` + id + `","name":"n","specification":{"specificationVersion":"v4","provider":"p","modelId":"m"},"modelType":"language"}]}`))
	}))
	defer server.Close()
	p, err := New(Config{APIKey: "k", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Unix(1000, 0)
	p.nowFn = func() time.Time { return clock }
	m, err := p.GetAvailableModels(context.Background())
	if err != nil || m.Models[0].ID != "a/v1" {
		t.Fatalf("first: %v %+v", err, m)
	}
	clock = clock.Add(p.cacheRefreshTime + time.Second)
	m, err = p.GetAvailableModels(context.Background())
	if err != nil || m.Models[0].ID != "a/v1" {
		t.Fatalf("expected stale v1, got %v %+v", err, m)
	}
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	m, _ = p.GetAvailableModels(context.Background())
	if m.Models[0].ID != "a/v2" {
		t.Fatalf("expected refreshed v2, got %+v", m)
	}
}

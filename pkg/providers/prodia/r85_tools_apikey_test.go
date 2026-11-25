package prodia

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS 7716e189ee: apiKey may be a function resolved before each request.
func TestR85toolsAPIKeyFunc(t *testing.T) {
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	t.Setenv("PRODIA_TOKEN", "env-key")
	n := 0
	p := New(Config{BaseURL: srv.URL, APIKeyFunc: func(context.Context) (string, error) {
		n++
		return []string{"k1", "k2"}[n-1], nil
	}})
	for i := 0; i < 2; i++ {
		if _, err := p.client.Do(context.Background(), internalhttp.Request{Method: http.MethodGet, Path: "/x"}); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	if len(got) != 2 || got[0] != "Bearer k1" || got[1] != "Bearer k2" {
		t.Fatalf("auth headers = %q", got)
	}
	mu.Unlock()

	sentinel := errors.New("no key")
	p = New(Config{BaseURL: srv.URL, APIKeyFunc: func(context.Context) (string, error) { return "", sentinel }})
	if _, err := p.client.Do(context.Background(), internalhttp.Request{Method: http.MethodGet, Path: "/x"}); !errors.Is(err, sentinel) {
		t.Fatalf("func error = %v", err)
	}
	p = New(Config{BaseURL: srv.URL, APIKey: "static", APIKeyFunc: func(context.Context) (string, error) { return "f", nil }})
	var invalid *providererrors.InvalidArgumentError
	if _, err := p.client.Do(context.Background(), internalhttp.Request{Method: http.MethodGet, Path: "/x"}); !errors.As(err, &invalid) {
		t.Fatalf("both set err = %v", err)
	}
}

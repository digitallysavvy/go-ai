package groq

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
)

// TS 7716e189ee: Config.APIKeyFunc resolves the key before each request,
// wins over the environment, and cannot be combined with APIKey.
func TestR85provaGroqAPIKeyFunc(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	t.Setenv("GROQ_API_KEY", "env-key")
	calls := 0
	p := New(Config{BaseURL: srv.URL, APIKeyFunc: func(ctx context.Context) (string, error) {
		calls++
		return fmt.Sprintf("key-%d", calls), nil
	}})
	for i := 0; i < 2; i++ {
		if _, err := p.client.Do(context.Background(), internalhttp.Request{Method: http.MethodGet, Path: "/r85"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 2 || got[0] != "Bearer key-1" || got[1] != "Bearer key-2" {
		t.Fatalf("auth headers = %q", got)
	}
	failing := New(Config{BaseURL: srv.URL, APIKeyFunc: func(context.Context) (string, error) { return "", errors.New("vault down") }})
	if _, err := failing.client.Do(context.Background(), internalhttp.Request{Method: http.MethodGet, Path: "/r85"}); err == nil || !strings.Contains(err.Error(), "vault down") {
		t.Fatalf("func error = %v", err)
	}
	both := New(Config{BaseURL: srv.URL, APIKey: "k", APIKeyFunc: func(context.Context) (string, error) { return "f", nil }})
	if _, err := both.client.Do(context.Background(), internalhttp.Request{Method: http.MethodGet, Path: "/r85"}); err == nil {
		t.Fatal("APIKey and APIKeyFunc both set: want an error")
	}
}

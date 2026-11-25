package http

import (
	"context"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Request bodies are serialized like TS JSON.stringify (postJsonToApi): `<`,
// `>` and `&` stay literal, U+2028/U+2029 are written as is, and there is no
// trailing newline. Go's json.Marshal would send <, > and &.
func TestJSONRequestBodyMatchesJSONStringify(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL})
	body := map[string]interface{}{"prompt": "<b>a & b</b> "}
	if _, err := c.Do(context.Background(), Request{Method: stdhttp.MethodPost, Path: "/do", Body: body}); err != nil {
		t.Fatal(err)
	}
	resp, err := c.DoStream(context.Background(), Request{Method: stdhttp.MethodPost, Path: "/stream", Body: body})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	want := "{\"prompt\":\"<b>a & b</b> \"}"
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("got %d requests", len(bodies))
	}
	for i, got := range bodies {
		if got != want {
			t.Fatalf("request %d body = %q, want %q", i, got, want)
		}
	}
}

package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type r91timeoutErr struct{}

func (r91timeoutErr) Error() string   { return "i/o timeout" }
func (r91timeoutErr) Timeout() bool   { return true }
func (r91timeoutErr) Temporary() bool { return true }

// B1 (OVC-02): TransportError is the exported transportError behavior.
func TestR91baseTransportError(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded, fmt.Errorf("wrap: %w", context.Canceled)} {
		if got := TransportError(err); got != err {
			t.Fatalf("TransportError(%v) = %v, want unchanged", err, got)
		}
	}
	if got := TransportError(r91timeoutErr{}); got.Error() != "Cannot connect to API: i/o timeout" {
		t.Fatalf("TransportError(timeout) = %v, want the retryable connect error", got)
	}

	rej := &FetchRejection{Err: errors.New("no credentials")}
	if got := TransportError(fmt.Errorf("Post x: %w", rej)); got != rej {
		t.Fatalf("FetchRejection not returned as is: %v", got)
	}

	cause := errors.New("dial tcp: connection refused")
	got := TransportError(cause)
	if got.Error() != "Cannot connect to API: dial tcp: connection refused" {
		t.Fatalf("text = %q", got.Error())
	}
	if !errors.Is(got, cause) {
		t.Fatal("cause not wrapped")
	}
	r, ok := got.(interface{ IsRetryable() bool })
	if !ok || !r.IsRetryable() {
		t.Fatal("connect error not retryable")
	}
	if transportError(cause).Error() != got.Error() {
		t.Fatal("transportError does not delegate")
	}
}

// B2 (prova-03): an ordered query keeps insertion order like URLSearchParams;
// without it the URL is built from Query as before.
func TestR91baseOrderedQuery(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.RawQuery)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL})
	ctx := context.Background()

	sp := NewSearchParams()
	sp.Append("z", "1")
	sp.Append("a", "x y~")
	sp.Set("z", "2")
	sp.Append("a", "3")
	reqs := []Request{
		{Method: http.MethodGet, Path: "/p", SearchParams: sp},
		{Method: http.MethodGet, Path: "/p", Query: map[string]string{"b": "1", "a": "2"}},
		{Method: http.MethodGet, Path: "/p", SearchParams: NewSearchParams(), Query: map[string]string{"b": "1", "a": "2"}},
		{Method: http.MethodGet, Path: "/p", SearchParams: sp, Query: map[string]string{"q": "1"}},
		{Method: http.MethodGet, Path: "/p"},
	}
	for _, req := range reqs {
		var out map[string]interface{}
		if err := c.DoJSON(ctx, req, &out); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := c.DoStream(ctx, reqs[0])
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	want := []string{
		"z=2&a=x+y%7E&a=3",
		"a=2&b=1",
		"a=2&b=1",
		"z=2&a=x+y%7E&a=3&q=1",
		"",
		"z=2&a=x+y%7E&a=3",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("queries = %q, want %q", got, want)
	}
	if (*SearchParams)(nil).Len() != 0 {
		t.Fatal("nil Len")
	}
}

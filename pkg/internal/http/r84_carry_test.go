package http

import (
	"context"
	"errors"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
)

// CR r84 stream->carry: TS createJsonResponseHandler parses response.text(),
// a WHATWG UTF-8 decode: a truncated sequence is one U+FFFD (Go's
// encoding/json writes one per invalid byte) and a leading BOM is dropped
// (encoding/json rejects it).
func TestR84carryJSONResponseBodyIsDecodedAsText(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"truncated sequence", "{\"v\":\"x\xF0\x9F\x98y\"}", "x�y"},
		{"leading BOM", "\xEF\xBB\xBF{\"v\":\"ok\"}", "ok"},
		{"valid", `{"v":"caf` + "é" + `"}`, "café"},
	} {
		srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(tc.body))
		}))
		var out struct {
			V string `json:"v"`
		}
		err := NewClient(Config{BaseURL: srv.URL}).GetJSON(context.Background(), "/", &out)
		srv.Close()
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if out.V != tc.want {
			t.Errorf("%s: v = %q, want %q", tc.name, out.V, tc.want)
		}
	}
}

// CRs r84 prova/provb->carry: DoJSONResponse returns the decoded text as the
// response body (providers re-read it), the validator sees it, and an error
// body is the text too (TS createJsonErrorResponseHandler reads
// response.text()).
func TestR84carryDoJSONResponseBodyIsText(t *testing.T) {
	status := 200
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte("\xEF\xBB\xBF{\"ok\":\"a\xE2\x82\"}"))
	}))
	defer srv.Close()
	const want = "{\"ok\":\"a�\"}"
	var validated string
	var out map[string]interface{}
	resp, err := NewClient(Config{BaseURL: srv.URL}).DoJSONResponse(context.Background(), Request{
		Method: nethttp.MethodGet, Path: "/",
		ValidateResponse: func(_ int, _ nethttp.Header, body []byte) error { validated = string(body); return nil },
	}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != want || validated != want || out["ok"] != "a�" {
		t.Fatalf("body %q, validated %q, out %v", resp.Body, validated, out)
	}
	status = 400
	err = NewClient(Config{BaseURL: srv.URL}).DoJSON(context.Background(), Request{Method: nethttp.MethodGet, Path: "/"}, &out)
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) || string(statusErr.Body) != want {
		t.Fatalf("error = %v", err)
	}
}

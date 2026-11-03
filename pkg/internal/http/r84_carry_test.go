package http

import (
	"context"
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

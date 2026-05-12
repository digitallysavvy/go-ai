package http

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientDoAndHelpers(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		switch r.URL.Path {
		case "/echo":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"method": r.Method,
				"query":  r.URL.Query(),
				"hdr":    r.Header.Get("X-Custom"),
			})
		case "/json":
			if r.Header.Get("Content-Type") != "application/json" {
				t.Fatalf("Content-Type = %q", r.Header.Get("Content-Type"))
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		case "/stream":
			w.WriteHeader(stdhttp.StatusOK)
			_, _ = w.Write([]byte("chunk"))
		case "/bad":
			w.WriteHeader(stdhttp.StatusBadRequest)
			_, _ = w.Write([]byte("bad req"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	underlying := &stdhttp.Client{Timeout: 3 * time.Second}
	c := NewClient(Config{
		BaseURL:    srv.URL,
		Headers:    map[string]string{"X-Default": "yes"},
		HTTPClient: underlying,
	})
	if c.HTTPClient() != underlying {
		t.Fatal("HTTPClient should expose configured underlying client")
	}

	resp, err := c.Do(context.Background(), Request{
		Method: stdhttp.MethodGet,
		Path:   "/echo",
		Headers: map[string]string{
			"X-Custom": "v",
		},
		Query: map[string]string{"a": "1", "b": "2"},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		t.Fatalf("unmarshal Do body: %v", err)
	}
	if out["method"] != "GET" || out["hdr"] != "v" {
		t.Fatalf("unexpected echo body: %#v", out)
	}

	var got map[string]interface{}
	if err := c.DoJSON(context.Background(), Request{
		Method: stdhttp.MethodPost,
		Path:   "/json",
		Body:   map[string]interface{}{"x": 1},
	}, &got); err != nil {
		t.Fatalf("DoJSON: %v", err)
	}
	if got["ok"] != true {
		t.Fatalf("DoJSON decoded payload: %#v", got)
	}

	got = map[string]interface{}{}
	raw, err := c.DoJSONResponse(context.Background(), Request{
		Method: stdhttp.MethodPost,
		Path:   "/json",
		Body:   []byte(`{"x":1}`),
	}, &got)
	if err != nil {
		t.Fatalf("DoJSONResponse: %v", err)
	}
	if raw.StatusCode != 200 || got["ok"] != true {
		t.Fatalf("DoJSONResponse mismatch raw=%#v got=%#v", raw, got)
	}

	streamResp, err := c.DoStream(context.Background(), Request{
		Method: stdhttp.MethodGet,
		Path:   "/stream",
	})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	streamBody, _ := io.ReadAll(streamResp.Body)
	_ = streamResp.Body.Close()
	if string(streamBody) != "chunk" {
		t.Fatalf("stream body = %q", string(streamBody))
	}

	if _, err := c.DoStream(context.Background(), Request{
		Method: stdhttp.MethodGet,
		Path:   "/bad",
	}); err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("DoStream expected HTTP status error, got %v", err)
	}

	if badResp, err := c.Post(context.Background(), "/bad", map[string]interface{}{"a": 1}); err != nil || badResp.StatusCode != stdhttp.StatusBadRequest {
		t.Fatalf("Post expected 400 response without transport error, got resp=%#v err=%v", badResp, err)
	}
	if err := c.PostJSON(context.Background(), "/json", map[string]interface{}{"a": 1}, &got); err != nil {
		t.Fatalf("PostJSON: %v", err)
	}
	if _, err := c.Get(context.Background(), "/echo"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := c.GetJSON(context.Background(), "/echo", &got); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}

	c.SetHeader("X-Test", "t")
	c.SetBaseURL(srv.URL)
}

func TestClientErrorPaths(t *testing.T) {
	c := NewClient(Config{BaseURL: "http://127.0.0.1:0"})

	if _, err := c.Do(context.Background(), Request{
		Method: stdhttp.MethodPost,
		Path:   "/x",
		Body:   make(chan int), // not JSON marshalable
	}); err == nil || !strings.Contains(err.Error(), "failed to marshal request body") {
		t.Fatalf("expected marshal error, got %v", err)
	}

	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusOK)
		_, _ = w.Write([]byte(`{not-json}`))
	}))
	defer srv.Close()

	c = NewClient(Config{BaseURL: srv.URL})
	var out map[string]interface{}
	if err := c.DoJSON(context.Background(), Request{Method: stdhttp.MethodGet, Path: "/"}, &out); err == nil {
		t.Fatal("DoJSON should fail for invalid response JSON")
	}
}

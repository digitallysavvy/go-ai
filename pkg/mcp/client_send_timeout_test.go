package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCallTimeoutAbortsSlowJSONReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&m)
		method, _ := m["method"].(string)
		if m["id"] == nil {
			w.WriteHeader(202)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(mustJSON(m["id"])) + `,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"s","version":"1"}}}`))
		default:
			time.Sleep(1500 * time.Millisecond)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(mustJSON(m["id"])) + `,"result":{"content":[]}}`))
		}
	}))
	defer srv.Close()
	f := false
	c := NewMCPClient(NewHTTPTransport(HTTPTransportConfig{URL: srv.URL}), MCPClientConfig{ProtocolVersionDiscovery: &f})
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Close() //nolint:errcheck
	ctx := WithRequestOptions(context.Background(), RequestOptions{TimeoutMS: 200})
	start := time.Now()
	_, err := c.CallTool(ctx, "x", nil)
	// TS arms the timeout before transport.send and aborts the send with it, so
	// a server that holds the POST open cannot outlast the call's timeout.
	var te *TimeoutError
	if !errors.As(err, &te) || te.Error() != "Request timed out after 200ms" {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 900*time.Millisecond {
		t.Fatalf("timeout not enforced: %v", time.Since(start))
	}
}

func mustJSON(v interface{}) []byte { b, _ := json.Marshal(v); return b }

// TS never reads the body of an accepted POST, so a server that keeps the
// response open cannot hold Send once the cap is read.
func TestSendDoesNotWaitForEndlessAcceptedBody(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusAccepted)
		chunk := make([]byte, 64<<10)
		fl := w.(http.Flusher)
		for {
			select {
			case <-done:
				return
			case <-r.Context().Done():
				return
			default:
			}
			if _, err := w.Write(chunk); err != nil {
				return
			}
			fl.Flush()
		}
	}))
	defer srv.Close()
	defer close(done)
	tr := NewHTTPTransport(HTTPTransportConfig{URL: srv.URL})
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tr.Close() //nolint:errcheck
	msg, err := CreateNotification("notifications/initialized", nil)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- tr.Send(context.Background(), msg) }()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Send is stuck reading an endless response body")
	}
}

package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TS response schemas declare counts as z.number(): a fractional count must
// not fail a non-stream decode into an int field (prova CR r79 proposal 6).
func TestR79integDecodeFractionalCounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","usage":{"prompt_tokens":1.5,"completion_tokens":2e1},"score":0.25}`))
	}))
	defer srv.Close()
	type body struct {
		ID    string `json:"id"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Score float64 `json:"score"`
	}
	c := NewClient(Config{BaseURL: srv.URL})
	for name, do := range map[string]func(*body) error{
		"DoJSON": func(b *body) error { return c.DoJSON(context.Background(), Request{Method: "GET", Path: "/"}, b) },
		"DoJSONResponse": func(b *body) error {
			_, err := c.DoJSONResponse(context.Background(), Request{Method: "GET", Path: "/"}, b)
			return err
		},
	} {
		var b body
		if err := do(&b); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if b.ID != "x" || b.Usage.PromptTokens != 1 || b.Usage.CompletionTokens != 20 || b.Score != 0.25 {
			t.Fatalf("%s: got %+v", name, b)
		}
	}
}

// A body that still fails after the retry reports the strict decode's error.
func TestR79integDecodeKeepsStrictError(t *testing.T) {
	var v struct {
		Name string `json:"name"`
	}
	err := decodeJSONBody([]byte(`{"name":1.5}`), &v)
	if err == nil || !strings.Contains(err.Error(), "Go struct field .name of type string") {
		t.Fatalf("got %v", err)
	}
}

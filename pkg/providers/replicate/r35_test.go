package replicate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A nil options pointer must not panic (the sibling models treat it as empty).
func TestR35_LanguageModelNilOptions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"p1","status":"succeeded","output":["hi"]}`))
	}))
	defer srv.Close()
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "owner/model")
	res, err := m.DoGenerate(context.Background(), nil)
	if err != nil || res.Text != "hi" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

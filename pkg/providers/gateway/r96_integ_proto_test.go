package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestR96integGatewayConfigPrototypeKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[],"__proto__":{}}`))
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.GetAvailableModels(context.Background())
	var jpe *providererrors.JSONParseError
	if err == nil || !errors.As(err, &jpe) {
		t.Fatalf("err = %T %v, want a gateway error caused by a JSONParseError", err, err)
	}
}

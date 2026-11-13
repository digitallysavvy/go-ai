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

// Round 96 integrator __proto__ sweep: the gateway lenient decode (V4
// generate results, stream parts) keeps the forbidden prototype failure of
// its strict decode instead of falling back to the key-by-key decode.
func TestR96integGatewayLenientDecodeKeepsPrototypeFailure(t *testing.T) {
	var wire struct {
		Text string `json:"text"`
	}
	err := decodeGatewayLenient([]byte(`{"text":1,"x":{"__proto__":{}}}`), &wire)
	var jpe *providererrors.JSONParseError
	if !errors.As(err, &jpe) {
		t.Fatalf("err = %T %v, want *JSONParseError", err, err)
	}
	if _, err := decodeV4GenerateResult([]byte(`{"content":[],"__proto__":{}}`)); !errors.As(err, &jpe) {
		t.Fatalf("decodeV4GenerateResult err = %T %v, want *JSONParseError", err, err)
	}
}

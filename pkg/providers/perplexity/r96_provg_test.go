package perplexity

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR96provgAgentResponsePrototypeKey: TS createJsonResponseHandler
// (secureJsonParse) fails a body with a __proto__ key: an "Invalid JSON
// response" whose cause is the JSONParseError.
func TestR96provgAgentResponsePrototypeKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","created_at":1,"model":"sonar","object":"response","status":"completed","output":[],"__proto__":{}}`))
	}))
	defer srv.Close()
	_, err := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL + "/"}), "sonar").DoGenerate(t.Context(), &provider.GenerateOptions{})
	var pe *providererrors.ProviderError
	var jpe *providererrors.JSONParseError
	if !errors.As(err, &pe) || pe.Message != "Invalid JSON response" || !errors.As(err, &jpe) {
		t.Fatalf("err = %T %v, want Invalid JSON response caused by a JSONParseError", err, err)
	}
}

// TestR96provgAgentStreamRawPrototypeKey: a frame that fails safeParseJSON
// has rawValue undefined in its raw part.
func TestR96provgAgentStreamRawPrototypeKey(t *testing.T) {
	body := "data: {\"type\":\"response.created\",\"__proto__\":{}}\n\n"
	s := newPerplexityAgentStream(io.NopCloser(strings.NewReader(body)), nil, true, nil)
	c, err := s.Next()
	for err == nil && c.Type != provider.ChunkTypeRaw {
		c, err = s.Next()
	}
	if err != nil || c.Raw != nil {
		t.Fatalf("raw part = %+v, %v; want a nil rawValue", c, err)
	}
}

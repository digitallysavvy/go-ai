package http

import (
	"context"
	"math"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// A JSON response body holding a number beyond the double range under a
// free-form value decodes, as JSON.parse reads it (+Inf).
func TestR96carryDecodeJSONBodyOutOfRange(t *testing.T) {
	var got struct {
		ID       string                 `json:"id"`
		Metadata map[string]interface{} `json:"metadata"`
	}
	if err := decodeJSONBody([]byte(`{"id":"a","metadata":{"v":1e400}}`), &got); err != nil {
		t.Fatalf("decodeJSONBody: %v", err)
	}
	if f, _ := got.Metadata["v"].(float64); got.ID != "a" || !math.IsInf(f, 1) {
		t.Errorf("got %#v", got)
	}
}

// TS createJsonResponseHandler (safeParseJSON) rejects a response body with a
// forbidden prototype key with a JSONParseError; DoJSON returns it.
func TestR96carryDoJSONForbiddenPrototype(t *testing.T) {
	body := `{"id":"a","__proto__":{"x":1}}`
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL})
	var out map[string]interface{}
	err := c.DoJSON(context.Background(), Request{Method: "GET", Path: "/"}, &out)
	if !providererrors.IsJSONParseError(err) || !strings.Contains(err.Error(), "JSON parsing failed: Text: "+body+".\nError message: SyntaxError: Object contains forbidden prototype property") {
		t.Fatalf("DoJSON = %v", err)
	}
	if _, err := c.DoJSONResponse(context.Background(), Request{Method: "GET", Path: "/"}, &out); !providererrors.IsJSONParseError(err) {
		t.Fatalf("DoJSONResponse = %v", err)
	}
}

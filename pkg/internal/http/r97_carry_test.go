package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createJsonResponseHandler: a __proto__ body fails safeParseJSON and
// becomes the APICallError "Invalid JSON response" the provider's handler
// builds (here: its ValidateResponse), caused by the JSONParseError. The
// prototype check ran before the validator, so the validator never saw it.
func TestR97carryValidateResponseRunsBeforePrototypeCheck(t *testing.T) {
	body := `{"id":"x","__proto__":{}}`
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL})
	invalid := errors.New("Invalid JSON response")
	validate := func(_ int, _ stdhttp.Header, b []byte) error {
		if err := jsonexact.ParseError(b); err != nil {
			return errors.Join(invalid, err)
		}
		return nil
	}
	var out map[string]any
	err := c.DoJSON(context.Background(), Request{Method: "GET", ValidateResponse: validate}, &out)
	if !errors.Is(err, invalid) || !providererrors.IsJSONParseError(err) {
		t.Fatalf("DoJSON err = %v, want the validator's error", err)
	}
	_, err = c.DoJSONResponse(context.Background(), Request{Method: "GET", ValidateResponse: validate}, &out)
	if !errors.Is(err, invalid) {
		t.Fatalf("DoJSONResponse err = %v, want the validator's error", err)
	}
	// A validator that passes the body, or none, still gets the
	// secureJsonParse rejection.
	pass := func(int, stdhttp.Header, []byte) error { return nil }
	for _, v := range []func(int, stdhttp.Header, []byte) error{pass, nil} {
		if err := c.DoJSON(context.Background(), Request{Method: "GET", ValidateResponse: v}, &out); !providererrors.IsJSONParseError(err) || !errors.Is(err, jsonexact.ErrForbiddenPrototype) {
			t.Fatalf("DoJSON err = %v, want JSONParseError", err)
		}
	}
}

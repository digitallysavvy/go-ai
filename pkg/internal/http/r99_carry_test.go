package http

import (
	"context"
	"errors"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func r99carryStatusServer(status int, body string) *httptest.Server {
	return httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("X-Request-Id", "r1")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

// fetch gives a 204 / 205 response a null body (node 24), so TS
// createEventSourceResponseHandler throws EmptyResponseBodyError and
// postToApi wraps it in the APICallError "Failed to process successful
// response" (status and response headers, no body, not retryable).
func TestR99carryDoStreamNullBodyStatus(t *testing.T) {
	for _, status := range []int{stdhttp.StatusNoContent, stdhttp.StatusResetContent} {
		srv := r99carryStatusServer(status, "")
		c := NewClient(Config{BaseURL: srv.URL})
		resp, err := c.DoStream(context.Background(), Request{Method: stdhttp.MethodPost, Path: "/x", Body: map[string]any{}})
		if resp != nil {
			_ = resp.Body.Close()
		}
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "Failed to process successful response" || pe.StatusCode != status ||
			pe.IsRetryable() || pe.ResponseBody != "" || pe.ResponseHeaders["x-request-id"] != "r1" {
			t.Fatalf("%d: err = %v (%+v)", status, err, pe)
		}
		var empty *providererrors.EmptyResponseBodyError
		if !errors.As(err, &empty) || empty.Error() != "Empty response body" {
			t.Fatalf("%d: cause = %v, want EmptyResponseBodyError", status, err)
		}
		// Every provider's fallback (NewProviderError) keeps it and names it.
		named := providererrors.NewProviderError("p", 0, "", err.Error(), err)
		if named.Message != pe.Message || named.StatusCode != status || named.Provider != "p" || pe.Provider != "" {
			t.Fatalf("%d: NewProviderError = %+v", status, named)
		}
		srv.Close()
	}
	// A 200 stream is still handed back.
	srv := r99carryStatusServer(stdhttp.StatusOK, "data: {}\n\n")
	defer srv.Close()
	resp, err := NewClient(Config{BaseURL: srv.URL}).DoStream(context.Background(), Request{Method: stdhttp.MethodPost, Path: "/x"})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

// TS createJsonResponseHandler: safeParseJSON of a body that is not JSON
// text (the empty text of a 204 / 205 null body, or of a 200 without a body)
// fails, and the handler throws the APICallError "Invalid JSON response"
// with the status, headers and body, caused by the JSONParseError. Go
// returned "failed to decode JSON response: unexpected end of JSON input"
// when the caller had no response validator.
func TestR99carryDoJSONNotJSONIsInvalidJSONResponse(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{stdhttp.StatusNoContent, ""},
		{stdhttp.StatusResetContent, ""},
		{stdhttp.StatusOK, ""},
		{stdhttp.StatusOK, "<html>"},
	} {
		srv := r99carryStatusServer(tc.status, tc.body)
		c := NewClient(Config{BaseURL: srv.URL})
		check := func(path string, err error) {
			t.Helper()
			var pe *providererrors.ProviderError
			if !errors.As(err, &pe) || pe.Message != "Invalid JSON response" || pe.StatusCode != tc.status ||
				pe.ResponseBody != tc.body || pe.ResponseHeader("x-request-id") != "r1" {
				t.Fatalf("%s %d %q: err = %v", path, tc.status, tc.body, err)
			}
			var parseErr *providererrors.JSONParseError
			if !errors.As(err, &parseErr) {
				t.Fatalf("%s %d: cause = %v, want the JSONParseError", path, tc.status, err)
			}
		}
		var out map[string]any
		check("DoJSON", c.DoJSON(context.Background(), Request{Method: stdhttp.MethodGet, Path: "/x"}, &out))
		_, err := c.DoJSONResponse(context.Background(), Request{Method: stdhttp.MethodGet, Path: "/x"}, &out)
		check("DoJSONResponse", err)
		srv.Close()
	}
}

package groq

import (
	"encoding/json"
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS groqErrorDataSchema requires error.message and error.type as strings
// and errorToMessage returns error.message, so "" is the message; a body
// missing either is not a Groq error (provb CR, class of 91b2205f).
func TestR87integGroqEmptyErrorMessageKept(t *testing.T) {
	cases := []struct {
		body    string
		wantNil bool
	}{
		{`{"error":{"message":"","type":"invalid_request_error"}}`, false},
		{`{"error":{"message":"m"}}`, true},
		{`{"error":{"type":"t"}}`, true},
		{`{"error":{"message":1,"type":"t"}}`, true},
	}
	for _, tc := range cases {
		err := parseGroqProviderError(&internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(tc.body)})
		if tc.wantNil {
			if err != nil {
				t.Errorf("%s: got %v, want no Groq error", tc.body, err)
			}
			continue
		}
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "" || pe.ErrorCode != "invalid_request_error" {
			t.Errorf("%s: got %#v", tc.body, err)
		}
	}
	se := newGroqStreamProviderErrorChunk(json.RawMessage(`{"message":"","type":"server_error"}`), nil)
	if se.Message != "" {
		t.Errorf("stream error message = %q, want empty", se.Message)
	}
	se = newGroqStreamProviderErrorChunk(json.RawMessage(`{"type":"server_error"}`), nil)
	if se.Message == "" {
		t.Error("a frame without a message must fall back to the frame text")
	}
}

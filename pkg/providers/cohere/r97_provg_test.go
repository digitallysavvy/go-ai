package cohere

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS parseJSON wraps the secureJsonParse rejection in one JSONParseError
// whose cause is the SyntaxError (cr-r97-prova-to-provg).
func TestR97provgStreamToolArgumentsSingleJSONParseError(t *testing.T) {
	sse := "data: {\"type\":\"tool-call-start\",\"index\":0,\"delta\":{\"message\":{\"tool_calls\":{\"id\":\"tc1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"a\\\":1,\\\"__proto__\\\":{}}\"}}}}}\n\n" +
		"data: {\"type\":\"tool-call-end\",\"index\":0,\"delta\":{}}\n\n"
	s := newCohereV2Stream(io.NopCloser(strings.NewReader(sse)))
	var lastErr error
	for i := 0; i < 10; i++ {
		if _, err := s.Next(); err != nil {
			lastErr = err
			break
		}
	}
	var parseErr *providererrors.JSONParseError
	if !errors.As(lastErr, &parseErr) || parseErr.Cause != jsonexact.ErrForbiddenPrototype {
		t.Fatalf("stream error = %#v, want one JSONParseError caused by the SyntaxError", lastErr)
	}
	if strings.Count(lastErr.Error(), "JSON parsing failed") != 1 {
		t.Fatalf("message = %q", lastErr.Error())
	}
}

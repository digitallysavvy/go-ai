package cohere

import (
	"errors"
	"io"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR96provgStreamToolArgumentsRejectPrototypeKeys: TS parseJSON uses
// secureJsonParse, so accumulated tool arguments with a __proto__ key fail
// the stream with a JSONParseError.
func TestR96provgStreamToolArgumentsRejectPrototypeKeys(t *testing.T) {
	sse := "data: {\"type\":\"tool-call-start\",\"index\":0,\"delta\":{\"message\":{\"tool_calls\":{\"id\":\"tc1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"__proto__\\\":{}}\"}}}}}\n\n" +
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
	if !errors.As(lastErr, &parseErr) {
		t.Fatalf("stream error = %v, want JSONParseError", lastErr)
	}
}

package cohere

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// A stream chunk with a __proto__ key is a JSONParseError error part (TS
// safeParseJSON), not a validation error.
func TestR96integCohereChunkPrototypeKeyIsJSONParseError(t *testing.T) {
	sse := "data: {\"type\":\"message-start\",\"id\":\"x\",\"__proto__\":{}}\n\n"
	s := newCohereV2Stream(io.NopCloser(strings.NewReader(sse)))
	for i := 0; i < 10; i++ {
		c, err := s.Next()
		if err != nil {
			t.Fatalf("no error part: %v", err)
		}
		if c.Type == provider.ChunkTypeError {
			var pe *providererrors.JSONParseError
			if !errors.As(c.Err, &pe) {
				t.Fatalf("error part = %T %v", c.Err, c.Err)
			}
			return
		}
	}
	t.Fatal("no error part")
}

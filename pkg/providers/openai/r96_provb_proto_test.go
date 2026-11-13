package openai

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR96provbResponsesStreamForbiddenPrototype: TS parseJsonEventStream
// fails a frame with a __proto__ key (secureJsonParse) as a JSONParseError
// part, and the stream finishes with error.
func TestR96provbResponsesStreamForbiddenPrototype(t *testing.T) {
	stream := newResponsesStreamWithMetadata(io.NopCloser(strings.NewReader(
		"data: {\"type\":\"response.output_text.delta\",\"item_id\":\"m\",\"output_index\":0,\"content_index\":0,\"delta\":\"x\",\"__proto__\":{}}\n\n")),
		false, "web_search", "openai", nil)
	defer stream.Close() //nolint:errcheck
	var sawErr bool
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeText:
			t.Fatal("text delta emitted")
		case provider.ChunkTypeError:
			var pe *providererrors.JSONParseError
			if !errors.As(c.Err, &pe) {
				t.Fatalf("error = %v (%T), want JSONParseError", c.Err, c.Err)
			}
			sawErr = true
		case provider.ChunkTypeFinish:
			if c.FinishReason != types.FinishReasonError {
				t.Fatalf("finish = %s, want error", c.FinishReason)
			}
		}
	}
	if !sawErr {
		t.Fatal("no error part")
	}
}

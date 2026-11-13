package gemini

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR96provgStreamFramePrototypeKeyIsJSONParseError: TS parses each frame
// with secureJsonParse, so a frame with a __proto__ key is a JSONParseError
// error part and the stream keeps reading.
func TestR96provgStreamFramePrototypeKeyIsJSONParseError(t *testing.T) {
	s := newStream(sseStream(
		`{"candidates":[{"content":{"parts":[{"text":"a"}]}}],"__proto__":{}}`,
		`{"candidates":[{"content":{"parts":[{"text":"b"}]},"finishReason":"STOP"}]}`,
	), Config{MetadataKey: "google"}, nil, nil, "")
	var sawParseErr bool
	var text string
	for i := 0; i < 50; i++ {
		c, err := s.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeError:
			var pe *providererrors.JSONParseError
			if !errors.As(c.Err, &pe) {
				t.Fatalf("error part = %T %v, want *JSONParseError", c.Err, c.Err)
			}
			sawParseErr = true
		case provider.ChunkTypeText:
			text += c.Text
		}
	}
	if !sawParseErr || text != "b" {
		t.Fatalf("parse error = %v, text = %q; want the error part and only the second frame's text", sawParseErr, text)
	}
}

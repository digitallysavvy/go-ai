package deepgram

import (
	"errors"
	"strings"
	"testing"

	"context"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
)

// TestR97provcProtoMessageSingleJSONParseError (cr-r97-prova-to-provc): TS
// safeParseJSON fails a __proto__ message with one JSONParseError whose cause
// is secure-json-parse's SyntaxError; Go nested two.
func TestR97provcProtoMessageSingleJSONParseError(t *testing.T) {
	s := &deepgramTranscriptionStream{Session: wsutil.NewSession[provider.TranscriptionStreamPart](context.Background())}
	var (
		duration, reported *float64
		language           string
		finals             []deepgramFinalText
		segments           []provider.TranscriptSegment
	)
	_, err := s.handleEvent(`{"type":"Results","__proto__":{}}`, deepgramStreamConfig{}, &duration, &reported, &language, &finals, &segments)
	var pe *providererrors.JSONParseError
	if !errors.As(err, &pe) || pe.Cause != jsonexact.ErrForbiddenPrototype {
		t.Fatalf("err = %#v, want a JSONParseError caused by ErrForbiddenPrototype", err)
	}
	if n := strings.Count(err.Error(), "JSON parsing failed"); n != 1 {
		t.Fatalf("err = %q, want the parse-failure text once", err)
	}
}

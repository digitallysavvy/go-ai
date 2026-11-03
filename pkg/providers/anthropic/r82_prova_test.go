package anthropic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS parseJsonEventStream: eventsource-parser dispatches an empty `data:`
// event (dataLines > 0) and safeParseJSON("") fails, so doStream enqueues an
// `error` part (AI_JSONParseError "Unexpected end of JSON input") and keeps
// reading. Go skipped empty data events.
func TestR82provaAnthropicEmptyDataEventIsParseError(t *testing.T) {
	frames := []string{
		`{"type":"message_start","message":{"id":"msg_1","model":"claude-3-haiku-20240307","usage":{"input_tokens":1,"output_tokens":0}}}`,
		``,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		`{"type":"message_stop"}`,
	}
	m, closeFn := fixtureModel(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", f)
		}
	})
	defer closeFn()
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var seq []provider.ChunkType
	var parseErr *providererrors.JSONParseError
	for _, c := range drain(t, s) {
		seq = append(seq, c.Type)
		if c.Type == provider.ChunkTypeError && !(errors.As(c.Err, &parseErr) && parseErr.Text == "") {
			t.Fatalf("error part = %v, want a JSONParseError of empty text", c.Err)
		}
	}
	errAt, finishAt := -1, -1
	for i, ct := range seq {
		switch ct {
		case provider.ChunkTypeError:
			errAt = i
		case provider.ChunkTypeFinish:
			finishAt = i
		}
	}
	if errAt < 0 || finishAt < errAt {
		t.Fatalf("parts = %v, want an error part and then a finish", seq)
	}
}

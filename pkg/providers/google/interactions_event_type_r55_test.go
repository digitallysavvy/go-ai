package google

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Every member of TS googleInteractionsEventSchema requires a string
// event_type, so a null, non-object or event_type-less frame is an error part
// (finish status failed) and reading continues.
func TestR55InteractionsStreamFrameWithoutStringEventTypeIsError(t *testing.T) {
	for _, data := range []string{`null`, `5`, `"x"`, `[]`, `{}`, `{"event_type":null}`} {
		t.Run(data, func(t *testing.T) {
			body := "data: " + data + "\n\n" +
				`data: {"event_type":"interaction.completed","interaction":{"id":"v1","status":"completed"}}` + "\n\n"
			s := newInteractionsEventStream(context.Background(), New(Config{APIKey: "k"}).client, io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
			var sawError bool
			var finish *provider.StreamChunk
			for _, c := range drainInteractions(t, s) {
				switch c.Type {
				case provider.ChunkTypeError:
					sawError = true
				case provider.ChunkTypeFinish:
					finish = c
				}
			}
			if !sawError {
				t.Fatal("no error part")
			}
			if finish == nil {
				t.Fatal("no finish part")
			}
		})
	}
}

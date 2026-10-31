package google

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS safeParseJSON leaves rawValue undefined for a payload that is not valid
// JSON, so the interactions raw chunk carries nil, not the payload text.
func TestR54IntegratorInteractionsRawChunkForUnparsablePayloadIsNil(t *testing.T) {
	body := strings.Join([]string{
		`data: {"event_type":"interaction.created","interaction":{"id":"v1","created":"2026-01-02T03:04:05Z","model":"gemini-2.5-flash"}}`,
		``,
		`data: not json`,
		``,
		``,
	}, "\n")
	s := newInteractionsEventStream(context.Background(), New(Config{APIKey: "k"}).client, io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	s.(*interactionsStream).includeRawChunks = true
	var raws []*provider.StreamChunk
	for _, c := range drainInteractions(t, s) {
		if c.Type == provider.ChunkTypeRaw {
			raws = append(raws, c)
		}
	}
	if len(raws) != 2 || raws[0].Raw == nil || raws[1].Raw != nil {
		t.Fatalf("raw chunks = %#v, want the parsed first event and nil for the unparsable one", raws)
	}
}

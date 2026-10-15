package google

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func drainInteractions(t *testing.T, s provider.TextStream) []*provider.StreamChunk {
	t.Helper()
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		out = append(out, c)
	}
}

func TestInteractionsStreamIncludeRawChunksAndMalformedEvent(t *testing.T) {
	t.Parallel()
	body := strings.Join([]string{
		`data: {"event_type":"interaction.created","interaction":{"id":"v1","created":"2026-01-02T03:04:05Z","model":"gemini-2.5-flash"}}`,
		``,
		`data: not json`,
		``,
	}, "\n")
	s := newInteractionsEventStream(context.Background(), New(Config{APIKey: "k"}).client, io.NopCloser(strings.NewReader(body)), "", nil, nil, nil, 0)
	s.(*interactionsStream).includeRawChunks = true
	chunks := drainInteractions(t, s)

	var types_ []provider.ChunkType
	for _, c := range chunks {
		types_ = append(types_, c.Type)
	}
	raw := 0
	var meta *provider.StreamChunk
	var finish *provider.StreamChunk
	for i, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeRaw:
			raw++
		case provider.ChunkTypeResponseMetadata:
			meta = c
		case provider.ChunkTypeError:
			if i == 0 || chunks[i-1].Type != provider.ChunkTypeRaw {
				t.Fatalf("error not preceded by its raw chunk: %v", types_)
			}
		case provider.ChunkTypeFinish:
			finish = c
		}
	}
	if raw != 2 {
		t.Fatalf("raw chunks = %d, want 2: %v", raw, types_)
	}
	if meta == nil || !meta.ResponseMetadata.Timestamp.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("response metadata = %#v", meta)
	}
	// A malformed event is an error part, not a terminal stream error, and
	// marks the interaction failed (TS finishStatus = 'failed').
	if finish == nil || finish.FinishReason != types.FinishReasonError {
		t.Fatalf("finish = %#v, want error finish", finish)
	}
}

func TestSynthesizedInteractionsStreamMetadataAndRaw(t *testing.T) {
	t.Parallel()
	resp := interactionsResponse{ID: "v1", Created: "2026-01-02T03:04:05Z", Model: "deep-research", Status: InteractionStatusCompleted}
	chunks := drainInteractions(t, newSynthesizedInteractionsStream(resp, nil, nil, true))
	if len(chunks) < 3 || chunks[0].Type != provider.ChunkTypeResponseMetadata || chunks[1].Type != provider.ChunkTypeRaw {
		var ts []provider.ChunkType
		for _, c := range chunks {
			ts = append(ts, c.Type)
		}
		t.Fatalf("chunk order = %v, want response-metadata, raw, ..., finish", ts)
	}
	if chunks[0].ResponseMetadata.ID != "v1" || chunks[0].ResponseMetadata.ModelID != "deep-research" || chunks[0].ResponseMetadata.Timestamp.IsZero() {
		t.Fatalf("metadata = %#v", chunks[0].ResponseMetadata)
	}
	for _, c := range drainInteractions(t, newSynthesizedInteractionsStream(resp, nil, nil, false)) {
		if c.Type == provider.ChunkTypeRaw {
			t.Fatal("raw chunk without includeRawChunks")
		}
	}
}

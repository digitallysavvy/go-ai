package ai

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// outputTextStage is the text-delta half of TS createOutputTransformStream
// (stream-text.ts 1061-1224), which streamText runs after the user transforms
// whenever it parses partial output (`output ?? text()` with the replay
// buffer, or an explicit output):
//
//   - only the step's first text block is staged (its id is taken from the
//     first text part of the step); other parts pass through;
//   - a delta of that block is held until the partial output it produces
//     changes, then one text-delta with all held text is published (so an
//     object output's deltas that do not change the partial are merged into
//     the next one that does);
//   - the published delta carries the block's latest provider metadata
//     (`chunk.providerMetadata ?? textProviderMetadata`), also when the delta
//     itself has none;
//   - an empty delta with provider metadata is forwarded as is;
//   - held text is published before the block's text-end and before
//     finish-step;
//   - the state resets at start-step and at retry boundaries.
type outputTextStage struct {
	hasFirst bool
	firstID  string
	pending  string
	metadata json.RawMessage
}

func (s *outputTextStage) reset() { *s = outputTextStage{} }

// stage folds one chunk that left the transform chain in and returns the
// chunks to emit, in order. publish is the partial-output tracker's decision
// for the same chunk (a changed partial from the step's first text block).
func (s *outputTextStage) stage(c provider.StreamChunk, publish bool) []provider.StreamChunk {
	switch c.Type {
	case provider.ChunkTypeStartStep:
		s.reset()
		return []provider.StreamChunk{c}
	case provider.ChunkTypeFinishStep:
		return append(s.flush(), c)
	case provider.ChunkTypeTextStart, provider.ChunkTypeText, provider.ChunkTypeTextEnd:
	default:
		return []provider.StreamChunk{c}
	}
	if !s.hasFirst {
		s.hasFirst = true
		s.firstID = c.ID
	} else if c.ID != s.firstID {
		return []provider.StreamChunk{c}
	}
	switch c.Type {
	case provider.ChunkTypeTextStart:
		return []provider.StreamChunk{c}
	case provider.ChunkTypeTextEnd:
		return append(s.flush(), c)
	}
	if len(c.ProviderMetadata) > 0 {
		s.metadata = c.ProviderMetadata
	}
	if c.Text == "" && len(c.ProviderMetadata) > 0 {
		return []provider.StreamChunk{c}
	}
	s.pending += c.Text
	if !publish {
		return nil
	}
	out := c
	out.ID = s.firstID
	out.Text = s.pending
	out.ProviderMetadata = s.metadata
	s.pending = ""
	return []provider.StreamChunk{out}
}

// flush publishes the held text of the first block, if any.
func (s *outputTextStage) flush() []provider.StreamChunk {
	if s.pending == "" {
		return nil
	}
	out := provider.StreamChunk{Type: provider.ChunkTypeText, ID: s.firstID, Text: s.pending, ProviderMetadata: s.metadata}
	s.pending = ""
	return []provider.StreamChunk{out}
}

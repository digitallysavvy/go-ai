package ai

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// partialOutputTracker mirrors TS createOutputTransformStream's partial-output
// state: the partial is parsed from the text of the step's FIRST text block
// only (later text blocks of the same step are ignored), the state resets at
// every start-step and retry boundary, and a partial is published only when
// its serialised value changes. In the transform path it is fed the chunks
// that leave the experimental_transform chain, so the partial follows the
// transformed text (TS applies the output stage after user transforms).
type partialOutputTracker struct {
	firstTextID string
	hasFirstID  bool
	text        string
	lastKey     string
	hasLast     bool
}

func (t *partialOutputTracker) reset() { *t = partialOutputTracker{} }

// observe folds one chunk in and reports a partial to publish, if any.
func (t *partialOutputTracker) observe(ctx context.Context, spec outputProcessor, c provider.StreamChunk) (partial any, publish bool, err error) {
	switch c.Type {
	case provider.ChunkTypeStartStep:
		t.reset()
		return nil, false, nil
	case provider.ChunkTypeText:
	default:
		return nil, false, nil
	}
	if !t.hasFirstID {
		t.hasFirstID = true
		t.firstTextID = c.ID
	} else if c.ID != t.firstTextID {
		return nil, false, nil
	}
	t.text += c.Text
	if c.Text == "" && len(c.ProviderMetadata) > 0 {
		return nil, false, nil
	}
	p, ok, perr := spec.parsePartialOutput(ctx, ParsePartialOutputOptions{Text: t.text})
	if perr != nil {
		return nil, false, perr
	}
	if !ok {
		return nil, false, nil
	}
	key, ok := partialOutputDedupKey(p)
	if !ok || (t.hasLast && key == t.lastKey) {
		return nil, false, nil
	}
	t.hasLast = true
	t.lastKey = key
	return p, true, nil
}

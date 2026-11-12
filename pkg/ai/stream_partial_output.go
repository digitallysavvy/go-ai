package ai

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// partialOutputTracker mirrors TS createOutputTransformStream's partial-output
// state: the partial is parsed from the text of the step's FIRST text block
// (the block of the step's first text part) only (later text blocks of the same step are ignored), the state resets at
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
	// arrayParser scans a built-in ArrayOutput's elements incrementally
	// (TS createArrayStreamParser, 91a67c2f56); arrayChecked reports that
	// it was set up for this stream state, lastArrayPartial is its last
	// published partial.
	arrayParser      *arrayOutputStreamParser
	arrayChecked     bool
	lastArrayPartial any
}

func (t *partialOutputTracker) reset() { *t = partialOutputTracker{} }

// observe folds one chunk in and reports a partial to publish, if any.
func (t *partialOutputTracker) observe(ctx context.Context, spec outputProcessor, c provider.StreamChunk) (partial any, publish bool, err error) {
	switch c.Type {
	case provider.ChunkTypeStartStep:
		t.reset()
		return nil, false, nil
	case provider.ChunkTypeText, provider.ChunkTypeTextStart, provider.ChunkTypeTextEnd:
	default:
		return nil, false, nil
	}
	// TS firstTextChunkId is the id of the step's first text part of any
	// kind (text-start, text-delta or text-end).
	if !t.hasFirstID {
		t.hasFirstID = true
		t.firstTextID = c.ID
	} else if c.ID != t.firstTextID {
		return nil, false, nil
	}
	if c.Type != provider.ChunkTypeText {
		return nil, false, nil
	}
	t.text += c.Text
	if c.Text == "" && len(c.ProviderMetadata) > 0 {
		return nil, false, nil
	}
	// Built-in arrays publish only changed snapshots, so a growing array is
	// not reparsed, revalidated or serialized on every chunk (TS
	// stream-text.ts createOutputTransformStream, 91a67c2f56).
	if !t.arrayChecked {
		t.arrayChecked = true
		t.arrayParser = newArrayOutputStreamParser(ctx, spec)
	}
	if t.arrayParser != nil {
		partial, publish, fallback := t.arrayParser.feed(ctx, c.Text)
		if !fallback {
			if publish {
				t.lastArrayPartial = partial
			}
			return partial, publish, nil
		}
		t.arrayParser = nil
		if t.lastArrayPartial != nil {
			if key, ok := partialOutputDedupKey(t.lastArrayPartial); ok {
				t.lastKey, t.hasLast = key, true
			}
		}
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

// arrayOutputPkgPath is this package's import path, to recognize the built-in
// ArrayOutput type.
var arrayOutputPkgPath = reflect.TypeOf(partialOutputTracker{}).PkgPath()

// arrayOutputStreamParser is StreamText's ArrayOutput stage of TS
// createArrayOutputStreamParser (util/array-output-stream.ts): it parses and
// validates each delimited element once, keeping the last element pending
// until another starts or the enclosing object closes (as the partial JSON
// parser drops an incomplete last element). A nonstandard envelope or
// malformed JSON falls back to the output's own partial parser. Invalid
// elements are skipped, as ArrayOutput's partial parser does.
type arrayOutputStreamParser struct {
	scanner        arrayElementScanner
	ctx            context.Context
	spec           outputProcessor
	sliceType      reflect.Type
	pending        []reflect.Value
	elements       reflect.Value
	publishedCount int
	published      bool
}

// newArrayOutputStreamParser returns the incremental parser for a built-in
// ArrayOutput, or nil for any other output.
func newArrayOutputStreamParser(ctx context.Context, spec outputProcessor) *arrayOutputStreamParser {
	t := reflect.TypeOf(spec)
	if t == nil || t.Kind() != reflect.Pointer || t.Elem().PkgPath() != arrayOutputPkgPath || !strings.HasPrefix(t.Elem().Name(), "arrayOutput[") {
		return nil
	}
	empty, ok, err := spec.parsePartialOutput(ctx, ParsePartialOutputOptions{Text: `{"elements":[]}`})
	if err != nil || !ok || empty == nil || reflect.TypeOf(empty).Kind() != reflect.Slice {
		return nil
	}
	sliceType := reflect.TypeOf(empty)
	return &arrayOutputStreamParser{spec: spec, sliceType: sliceType, elements: reflect.MakeSlice(sliceType, 0, 0)}
}

func (p *arrayOutputStreamParser) elementStart() { p.publishPending() }
func (p *arrayOutputStreamParser) closed()       { p.publishPending() }

// elementDone parses and validates the element text once: false means
// malformed JSON (fall back). A schema failure skips the element.
func (p *arrayOutputStreamParser) elementDone(text string) bool {
	if !json.Valid([]byte(text)) {
		return false
	}
	// The output's own partial parser validates (with defaults) and types a
	// one-element array: the element's schema check and conversion, once.
	parsed, ok, err := p.spec.parsePartialOutput(p.ctx, ParsePartialOutputOptions{Text: `{"elements":[` + text + `]}`})
	if err != nil {
		return false
	}
	if ok {
		if v := reflect.ValueOf(parsed); v.Kind() == reflect.Slice && v.Len() == 1 {
			p.pending = []reflect.Value{v.Index(0)}
		}
	}
	return true
}

func (p *arrayOutputStreamParser) publishPending() {
	if len(p.pending) > 0 {
		p.elements = reflect.Append(p.elements, p.pending...)
	}
	p.pending = nil
}

// feed scans textDelta and returns a snapshot to publish when the element
// count changed, or fallback for a nonstandard envelope or malformed JSON.
func (p *arrayOutputStreamParser) feed(ctx context.Context, textDelta string) (partial any, publish bool, fallback bool) {
	p.ctx = ctx
	fallback = p.scanner.scan(textDelta, p)
	p.ctx = nil
	if fallback {
		return nil, false, true
	}
	if p.scanner.inPrefix() || (p.published && p.publishedCount == p.elements.Len()) {
		return nil, false, false
	}
	p.published = true
	p.publishedCount = p.elements.Len()
	// A snapshot keeps later chunks from changing an emitted array.
	snapshot := reflect.MakeSlice(p.sliceType, p.elements.Len(), p.elements.Len())
	reflect.Copy(snapshot, p.elements)
	return snapshot.Interface(), true, false
}

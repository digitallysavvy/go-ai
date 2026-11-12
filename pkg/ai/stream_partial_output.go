package ai

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"sync"

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

// partialLog records every partial output StreamText's output stage publishes,
// in order, so an element stream reads the same partial sequence TS
// createElementStreamTransform reads from createOutputTransformStream
// (91a67c2f56): parsed incrementally, from the step's first text block, and
// reset at step and stream retry boundaries, which the public stream does not
// mark. Readers replay it from the start and block for values not published
// yet; close ends it with the run's error.
type partialLog struct {
	mu     sync.Mutex
	cond   *sync.Cond
	values []any
	done   bool
	err    error
}

func newPartialLog() *partialLog {
	l := &partialLog{}
	l.cond = sync.NewCond(&l.mu)
	return l
}

func (l *partialLog) add(v any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.values = append(l.values, v)
	l.mu.Unlock()
	l.cond.Broadcast()
}

func (l *partialLog) close(err error) {
	if l == nil {
		return
	}
	l.mu.Lock()
	if !l.done {
		l.done, l.err = true, err
	}
	l.mu.Unlock()
	l.cond.Broadcast()
}

// wake releases readers blocked in next so they can check stop again.
func (l *partialLog) wake() { l.cond.Broadcast() }

// next returns the i-th published partial, blocking until it exists, the log
// closes (ok false, with the run's error) or stop reports true.
func (l *partialLog) next(i int, stop func() bool) (v any, ok bool, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i >= len(l.values) && !l.done && (stop == nil || !stop()) {
		l.cond.Wait()
	}
	if i < len(l.values) {
		return l.values[i], true, nil
	}
	return nil, false, l.err
}

// partialOutputLog returns the result's partial log when spec is the output
// the call parses its partial outputs with, else nil.
func (r *StreamTextResult) partialOutputLog(spec any) *partialLog {
	if r == nil || r.partials == nil || spec == nil {
		return nil
	}
	if !sameOutputValue(spec, r.partialsOutput) {
		return nil
	}
	return r.partials
}

// sameOutputValue reports a == b for Output values, false when their dynamic
// type is not comparable.
func sameOutputValue(a, b any) (same bool) {
	defer func() {
		if recover() != nil {
			same = false
		}
	}()
	return a != nil && a == b
}

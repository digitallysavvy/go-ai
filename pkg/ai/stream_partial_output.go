package ai

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf16"

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

type arrayStreamPhase int

const (
	arrayStreamPrefix arrayStreamPhase = iota
	arrayStreamElements
	arrayStreamSuffix
	arrayStreamDone
)

// jsWhitespace is the JS RegExp \s class.
const jsWhitespace = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

var arrayStreamPrefixPattern = regexp.MustCompile(`^[` + jsWhitespace + `]*\{[` + jsWhitespace + `]*"elements"[` + jsWhitespace + `]*:[` + jsWhitespace + `]*\[$`)

func isJSWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// arrayOutputStreamParser is the TS createArrayOutputStreamParser
// (util/array-output-stream.ts): it scans only new text and parses and
// validates each delimited element once, keeping the last element pending
// until another starts or the enclosing object closes (as the partial JSON
// parser drops an incomplete last element). A nonstandard envelope or
// malformed JSON falls back to the output's own partial parser. Invalid
// elements are skipped, as ArrayOutput's partial parser does.
type arrayOutputStreamParser struct {
	spec           outputProcessor
	sliceType      reflect.Type
	phase          arrayStreamPhase
	prefix         strings.Builder
	prefixUnits    int
	elementText    strings.Builder
	depth          int
	inString       bool
	escaped        bool
	afterComma     bool
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

// finishElement parses and validates the element text once: false means
// malformed JSON (fall back). A schema failure skips the element.
func (p *arrayOutputStreamParser) finishElement(ctx context.Context) bool {
	text := p.elementText.String()
	if !json.Valid([]byte(text)) {
		return false
	}
	// The output's own partial parser validates (with defaults) and types a
	// one-element array: the element's schema check and conversion, once.
	parsed, ok, err := p.spec.parsePartialOutput(ctx, ParsePartialOutputOptions{Text: `{"elements":[` + text + `]}`})
	if err != nil {
		return false
	}
	if ok {
		if v := reflect.ValueOf(parsed); v.Kind() == reflect.Slice && v.Len() == 1 {
			p.pending = []reflect.Value{v.Index(0)}
		}
	}
	p.elementText.Reset()
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
	for _, char := range textDelta {
		if p.phase == arrayStreamPrefix {
			p.prefix.WriteRune(char)
			p.prefixUnits += len(utf16.Encode([]rune{char}))
			if char == '[' {
				// Bound prefix scanning; other object shapes keep the
				// output's own partial parser.
				if !arrayStreamPrefixPattern.MatchString(p.prefix.String()) {
					return nil, false, true
				}
				p.phase = arrayStreamElements
			} else if p.prefixUnits > 256 {
				return nil, false, true
			}
			continue
		}

		if p.phase == arrayStreamSuffix || p.phase == arrayStreamDone {
			if isJSWhitespace(char) {
				continue
			}
			if p.phase == arrayStreamSuffix && char == '}' {
				p.publishPending()
				p.phase = arrayStreamDone
				continue
			}
			return nil, false, true
		}

		if !p.inString && p.depth == 0 && (char == ',' || char == ']') {
			if p.elementText.Len() > 0 {
				if !p.finishElement(ctx) {
					return nil, false, true
				}
			} else if char == ',' || p.afterComma {
				return nil, false, true
			}
			p.afterComma = char == ','
			if char == ']' {
				p.phase = arrayStreamSuffix
			}
			continue
		}

		if p.elementText.Len() == 0 {
			if isJSWhitespace(char) {
				continue
			}
			p.publishPending()
			p.afterComma = false
		}
		p.elementText.WriteRune(char)

		switch {
		case p.inString:
			if p.escaped {
				p.escaped = false
			} else if char == '\\' {
				p.escaped = true
			} else if char == '"' {
				p.inString = false
			}
		case char == '"':
			p.inString = true
		case char == '{' || char == '[':
			p.depth++
		case char == '}' || char == ']':
			p.depth--
		}
	}

	if p.phase == arrayStreamPrefix || (p.published && p.publishedCount == p.elements.Len()) {
		return nil, false, false
	}
	p.published = true
	p.publishedCount = p.elements.Len()
	// A snapshot keeps later chunks from changing an emitted array.
	snapshot := reflect.MakeSlice(p.sliceType, p.elements.Len(), p.elements.Len())
	reflect.Copy(snapshot, p.elements)
	return snapshot.Interface(), true, false
}

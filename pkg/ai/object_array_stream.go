package ai

import (
	"github.com/digitallysavvy/go-ai/pkg/jsonparser"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// arrayStreamResult is the outcome of one text delta: fallback means the
// text is not the standard envelope (or is malformed), and the caller goes
// back to reparsing the accumulated text; changed means a new partial array
// is published.
type arrayStreamResult struct {
	fallback bool
	changed  bool
	partial  []interface{}
}

// objectArrayStreamParser is StreamObject's array mode of TS
// createArrayOutputStreamParser (util/array-output-stream.ts, 91a67c2f56):
// it parses and validates each delimited element once, keeping the last
// element pending until another starts or the enclosing object closes. The
// partials it publishes equal the ones reparsing the accumulated text
// gives, without the quadratic cost on long arrays. An invalid element
// falls back (skipInvalidElements false).
type objectArrayStreamParser struct {
	scanner        arrayElementScanner
	schema         schema.Schema
	pending        []interface{}
	elements       []interface{}
	publishedCount int
	published      bool
}

func newObjectArrayStreamParser(s schema.Schema) *objectArrayStreamParser {
	return &objectArrayStreamParser{schema: s}
}

func (p *objectArrayStreamParser) elementStart() { p.publishPending() }
func (p *objectArrayStreamParser) closed()       { p.publishPending() }

// elementDone parses and validates the element text (TS safeParseJSON +
// safeValidateTypes); false means fallback.
func (p *objectArrayStreamParser) elementDone(text string) bool {
	parsed := jsonparser.ParsePartialJSON(text)
	if parsed.State != jsonparser.ParseStateSuccessful {
		return false
	}
	value := parsed.Value
	if p.schema != nil {
		value = schema.ApplyDefaults(value, p.schema)
		if err := p.schema.Validator().Validate(value); err != nil {
			return false
		}
	}
	p.pending = []interface{}{value}
	return true
}

func (p *objectArrayStreamParser) publishPending() {
	p.elements = append(p.elements, p.pending...)
	p.pending = nil
}

func (p *objectArrayStreamParser) next(textDelta string) arrayStreamResult {
	if p.scanner.scan(textDelta, p) {
		return arrayStreamResult{fallback: true}
	}
	if p.scanner.inPrefix() || (p.published && p.publishedCount == len(p.elements)) {
		return arrayStreamResult{}
	}
	p.published = true
	p.publishedCount = len(p.elements)
	// A snapshot: later chunks never change an array already handed out.
	return arrayStreamResult{changed: true, partial: append([]interface{}{}, p.elements...)}
}

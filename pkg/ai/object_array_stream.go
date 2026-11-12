package ai

import (
	"regexp"
	"unicode/utf16"

	"github.com/digitallysavvy/go-ai/pkg/jsonparser"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// arrayStreamPrefix is the standard array output envelope up to the
// elements array (TS /^\s*\{\s*"elements"\s*:\s*\[$/, \s being the JS
// white space class).
var arrayStreamPrefix = regexp.MustCompile(`^` + jsWhiteSpaceClass + `*\{` + jsWhiteSpaceClass + `*"elements"` + jsWhiteSpaceClass + `*:` + jsWhiteSpaceClass + `*\[$`)

// jsWhiteSpaceClass is the JS RegExp \s class as an RE2 class.
const jsWhiteSpaceClass = `[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

type arrayStreamPhase int

const (
	arrayStreamPrefixPhase arrayStreamPhase = iota
	arrayStreamElementsPhase
	arrayStreamSuffixPhase
	arrayStreamDonePhase
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

// arrayOutputStreamParser is the TS createArrayOutputStreamParser
// (util/array-output-stream.ts, 91a67c2f56): it scans only new text and
// parses and validates each delimited element once, keeping the last element
// pending until another starts or the enclosing object closes. The partials
// it publishes equal the ones reparsing the accumulated text gives, without
// the quadratic cost on long arrays. Parser state belongs to one stream.
type arrayOutputStreamParser struct {
	schema         schema.Schema
	phase          arrayStreamPhase
	prefix         []rune
	prefixUnits    int
	elementText    []rune
	depth          int
	inString       bool
	escaped        bool
	afterComma     bool
	pending        []interface{}
	elements       []interface{}
	publishedCount int
	published      bool
}

func newArrayOutputStreamParser(s schema.Schema) *arrayOutputStreamParser {
	return &arrayOutputStreamParser{schema: s}
}

// finishElement parses and validates the element text (TS safeParseJSON +
// safeValidateTypes, skipInvalidElements false); false means fallback.
func (p *arrayOutputStreamParser) finishElement() bool {
	parsed := jsonparser.ParsePartialJSON(string(p.elementText))
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
	p.elementText = p.elementText[:0]
	return true
}

func (p *arrayOutputStreamParser) publishPending() {
	p.elements = append(p.elements, p.pending...)
	p.pending = nil
}

func (p *arrayOutputStreamParser) next(textDelta string) arrayStreamResult {
	for _, char := range textDelta {
		if p.phase == arrayStreamPrefixPhase {
			p.prefix = append(p.prefix, char)
			p.prefixUnits += utf16.RuneLen(char)
			if char == '[' {
				// Bound prefix scanning; other object shapes keep the
				// reparsing path.
				if !arrayStreamPrefix.MatchString(string(p.prefix)) {
					return arrayStreamResult{fallback: true}
				}
				p.phase = arrayStreamElementsPhase
			} else if p.prefixUnits > 256 {
				return arrayStreamResult{fallback: true}
			}
			continue
		}

		if p.phase == arrayStreamSuffixPhase || p.phase == arrayStreamDonePhase {
			if providerutils.IsJSWhiteSpace(char) {
				continue
			}
			if p.phase == arrayStreamSuffixPhase && char == '}' {
				p.publishPending()
				p.phase = arrayStreamDonePhase
				continue
			}
			return arrayStreamResult{fallback: true}
		}

		if !p.inString && p.depth == 0 && (char == ',' || char == ']') {
			if len(p.elementText) > 0 {
				if !p.finishElement() {
					return arrayStreamResult{fallback: true}
				}
			} else if char == ',' || p.afterComma {
				return arrayStreamResult{fallback: true}
			}
			p.afterComma = char == ','
			if char == ']' {
				p.phase = arrayStreamSuffixPhase
			}
			continue
		}

		if len(p.elementText) == 0 {
			if providerutils.IsJSWhiteSpace(char) {
				continue
			}
			p.publishPending()
			p.afterComma = false
		}
		p.elementText = append(p.elementText, char)

		switch {
		case p.inString:
			switch {
			case p.escaped:
				p.escaped = false
			case char == '\\':
				p.escaped = true
			case char == '"':
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

	if p.phase == arrayStreamPrefixPhase || (p.published && p.publishedCount == len(p.elements)) {
		return arrayStreamResult{}
	}
	p.published = true
	p.publishedCount = len(p.elements)
	// A snapshot: later chunks never change an array already handed out.
	return arrayStreamResult{changed: true, partial: append([]interface{}{}, p.elements...)}
}

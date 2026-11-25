package ai

import (
	"regexp"
	"unicode/utf16"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// jsWhiteSpaceClass is the JS RegExp \s class as an RE2 class.
const jsWhiteSpaceClass = `[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

// arrayStreamPrefix is the standard array output envelope up to the
// elements array (TS /^\s*\{\s*"elements"\s*:\s*\[$/).
var arrayStreamPrefix = regexp.MustCompile(`^` + jsWhiteSpaceClass + `*\{` + jsWhiteSpaceClass + `*"elements"` + jsWhiteSpaceClass + `*:` + jsWhiteSpaceClass + `*\[$`)

type arrayStreamPhase int

const (
	arrayStreamPrefixPhase arrayStreamPhase = iota
	arrayStreamElementsPhase
	arrayStreamSuffixPhase
	arrayStreamDonePhase
)

// arrayElementHandler receives the scanner's element events: elementStart
// when a new element begins (the previous one is then complete, TS
// publishes pending), elementDone with a delimited element's text (false
// falls back to reparsing), and closed when the enclosing object ends.
type arrayElementHandler interface {
	elementStart()
	elementDone(text string) bool
	closed()
}

// arrayElementScanner is the scanning half of TS
// createArrayOutputStreamParser (util/array-output-stream.ts, 91a67c2f56),
// shared by StreamText's ArrayOutput stage and StreamObject's array mode: it
// reads only new text and finds the top-level elements of the standard
// `{"elements":[...]}` envelope. Parsing, validation and publishing belong
// to the caller. State belongs to one stream.
type arrayElementScanner struct {
	phase       arrayStreamPhase
	prefix      []rune
	prefixUnits int
	elementText []rune
	depth       int
	inString    bool
	escaped     bool
	afterComma  bool
}

// inPrefix reports that the elements array has not started yet.
func (s *arrayElementScanner) inPrefix() bool { return s.phase == arrayStreamPrefixPhase }

// scan feeds textDelta through the scanner; true means fallback (a
// nonstandard envelope or malformed JSON).
func (s *arrayElementScanner) scan(textDelta string, h arrayElementHandler) (fallback bool) {
	for _, char := range textDelta {
		if s.phase == arrayStreamPrefixPhase {
			s.prefix = append(s.prefix, char)
			s.prefixUnits += utf16.RuneLen(char)
			if char == '[' {
				// Bound prefix scanning; other object shapes keep the
				// reparsing path.
				if !arrayStreamPrefix.MatchString(string(s.prefix)) {
					return true
				}
				s.phase = arrayStreamElementsPhase
			} else if s.prefixUnits > 256 {
				return true
			}
			continue
		}

		if s.phase == arrayStreamSuffixPhase || s.phase == arrayStreamDonePhase {
			if providerutils.IsJSWhiteSpace(char) {
				continue
			}
			if s.phase == arrayStreamSuffixPhase && char == '}' {
				h.closed()
				s.phase = arrayStreamDonePhase
				continue
			}
			return true
		}

		if !s.inString && s.depth == 0 && (char == ',' || char == ']') {
			if len(s.elementText) > 0 {
				if !h.elementDone(string(s.elementText)) {
					return true
				}
				s.elementText = s.elementText[:0]
			} else if char == ',' || s.afterComma {
				return true
			}
			s.afterComma = char == ','
			if char == ']' {
				s.phase = arrayStreamSuffixPhase
			}
			continue
		}

		if len(s.elementText) == 0 {
			if providerutils.IsJSWhiteSpace(char) {
				continue
			}
			h.elementStart()
			s.afterComma = false
		}
		s.elementText = append(s.elementText, char)

		switch {
		case s.inString:
			switch {
			case s.escaped:
				s.escaped = false
			case char == '\\':
				s.escaped = true
			case char == '"':
				s.inString = false
			}
		case char == '"':
			s.inString = true
		case char == '{' || char == '[':
			s.depth++
		case char == '}' || char == ']':
			s.depth--
		}
	}
	return false
}

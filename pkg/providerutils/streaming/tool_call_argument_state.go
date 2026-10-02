package streaming

import (
	"strings"
	"unicode"
)

// StartsWithStructuredValue reports whether a streamed tool-call argument
// fragment begins a JSON object or array once leading whitespace is
// ignored. Ports TS startsWithStructuredValue
// (provider-utils/src/streaming-tool-call-argument-state.ts).
func StartsWithStructuredValue(value string) bool {
	trimmed := strings.TrimLeftFunc(value, unicode.IsSpace)
	if trimmed == "" {
		return false
	}
	switch trimmed[0] {
	case '{', '[':
		return true
	default:
		return false
	}
}

// argumentStructureKind mirrors TS's ArgumentStructure discriminated union
// ('undetermined' | 'other' | 'structured').
type argumentStructureKind int

const (
	argumentStructureUndetermined argumentStructureKind = iota
	argumentStructureOther
	argumentStructureStructured
)

// StreamingToolCallArgumentState incrementally tracks whether streamed
// tool-call arguments contain a complete structured JSON value. This is
// intentionally structural rather than a JSON parse: a currently parsable
// scalar can still be the prefix of a later value. Ports TS
// StreamingToolCallArgumentState
// (provider-utils/src/streaming-tool-call-argument-state.ts).
type StreamingToolCallArgumentState struct {
	kind     argumentStructureKind
	stack    []byte // each element is '{' or '['
	inString bool
	escaped  bool
	complete bool
}

// NewStreamingToolCallArgumentState creates a new argument state, seeding it
// with any argument text already observed.
func NewStreamingToolCallArgumentState(initialValue string) *StreamingToolCallArgumentState {
	s := &StreamingToolCallArgumentState{}
	s.Append(initialValue)
	return s
}

// HasCompleteStructuredValue reports whether the accumulated arguments form
// a complete JSON object or array.
func (s *StreamingToolCallArgumentState) HasCompleteStructuredValue() bool {
	return s.kind == argumentStructureStructured && s.complete
}

// Append incorporates the next argument delta into the tracked structure.
func (s *StreamingToolCallArgumentState) Append(delta string) {
	for _, r := range delta {
		switch s.kind {
		case argumentStructureUndetermined:
			if unicode.IsSpace(r) {
				continue
			}
			if r != '{' && r != '[' {
				s.kind = argumentStructureOther
				continue
			}
			s.kind = argumentStructureStructured
			s.stack = []byte{byte(r)}
			s.inString = false
			s.escaped = false
			s.complete = false

		case argumentStructureOther:
			// Once a value is known not to be structured, nothing can
			// recover it; ignore the remainder of the stream.

		case argumentStructureStructured:
			if s.complete {
				continue
			}
			if s.inString {
				switch {
				case s.escaped:
					s.escaped = false
				case r == '\\':
					s.escaped = true
				case r == '"':
					s.inString = false
				}
				continue
			}
			switch r {
			case '"':
				s.inString = true
			case '{', '[':
				s.stack = append(s.stack, byte(r))
			case '}', ']':
				expected := byte('{')
				if r == ']' {
					expected = '['
				}
				if len(s.stack) == 0 || s.stack[len(s.stack)-1] != expected {
					// Mismatched closing bracket: the value can never be
					// recovered as complete structured JSON.
					s.kind = argumentStructureOther
					s.stack = nil
					continue
				}
				s.stack = s.stack[:len(s.stack)-1]
				if len(s.stack) == 0 {
					s.complete = true
				}
			}
		}
	}
}

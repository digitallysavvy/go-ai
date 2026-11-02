package telemetry

import (
	"slices"

	"go.opentelemetry.io/otel/trace"
)

// spanSet is TS's `Map<string, {span, context}>` for a call's open tool and
// embedding spans (state.toolSpans, state.embedSpans): spans keyed by id that
// onError/onAbort end in insertion order (`for (... of map.values())`). A Go
// map would end them in random order. The zero value is an empty set; the
// caller holds the call state's mutex.
type spanSet struct {
	keys  []string
	spans map[string]trace.Span
}

// set records span under key. Like Map.set, replacing an existing key keeps
// its original position.
func (s *spanSet) set(key string, span trace.Span) {
	if s.spans == nil {
		s.spans = make(map[string]trace.Span)
	}
	if _, ok := s.spans[key]; !ok {
		s.keys = append(s.keys, key)
	}
	s.spans[key] = span
}

// take removes and returns the span recorded under key, or nil.
func (s *spanSet) take(key string) trace.Span {
	span, ok := s.spans[key]
	if !ok {
		return nil
	}
	delete(s.spans, key)
	if i := slices.Index(s.keys, key); i >= 0 {
		s.keys = slices.Delete(s.keys, i, i+1)
	}
	return span
}

// drain removes every span and returns them in insertion order (TS
// `map.values()` followed by `map.clear()`).
func (s *spanSet) drain() []trace.Span {
	out := make([]trace.Span, 0, len(s.keys))
	for _, k := range s.keys {
		out = append(out, s.spans[k])
	}
	s.keys, s.spans = nil, nil
	return out
}

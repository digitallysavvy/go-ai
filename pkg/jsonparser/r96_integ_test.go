package jsonparser

import (
	"errors"
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
)

// gen CR 2 (round 96): TS parsePartialJson runs safeParseJSON in both
// phases, so a forbidden prototype key fails the parse, and a number beyond
// the double range reads as Infinity (JSON.parse).
func TestR96integParsePartialJSONSecure(t *testing.T) {
	for _, text := range []string{`{"__proto__":{}}`, `{"a":{"__proto__":{}`, `{"constructor":{"prototype":{}}}`} {
		r := ParsePartialJSON(text)
		if r.State != ParseStateFailed || !errors.Is(r.Error, securejson.ErrForbiddenPrototype) {
			t.Errorf("%s: state = %s, err = %v, want failed-parse with the forbidden prototype error", text, r.State, r.Error)
		}
	}
	r := ParsePartialJSON(`{"a":1e400}`)
	m, _ := r.Value.(map[string]interface{})
	if r.State != ParseStateSuccessful || !math.IsInf(m["a"].(float64), 1) {
		t.Fatalf("result = %+v, want a = +Inf", r)
	}
}

package tsfixture

import (
	"reflect"
	"testing"
)

// The extended assertion forms (EvalExtendedAsserts) evaluate like JS.
func TestR84provbExtendedEvalPath(t *testing.T) {
	EvalExtendedAsserts(t)
	roots := map[string]interface{}{
		"result": map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "a", "citations": []interface{}{"c1", "c2"}},
				map[string]interface{}{"type": "source", "sourceType": "url", "url": "u1"},
				map[string]interface{}{"type": "source", "sourceType": "document", "title": "d"},
				map[string]interface{}{"type": "tool-call", "toolName": "x", "input": `{"call_id":"c9","n":1}`},
			},
		},
	}
	aliases := map[string]string{
		"sources": "result.content.filter(part => part.type === 'source' && part.sourceType === 'url')",
		"call":    "result.content.find(p => p.type === 'tool-call')",
	}
	for _, tc := range []struct {
		expr string
		want interface{}
	}{
		{"sources.length", float64(1)},
		{"sources.map(s => s.url)", []interface{}{"u1"}},
		{"result.content.flatMap(p => p.citations ?? [])", []interface{}{"c1", "c2"}},
		{"result.content.map(p => p.url ?? p.type)", []interface{}{"text", "u1", "source", "tool-call"}},
		{"result.content.filter(p => p.type !== 'source').length", float64(2)},
		{"result.content.filter(p => p.type.startsWith('tool')).length", float64(1)},
		{"result.content.every(p => p.type)", true},
		{"result.content.slice(1, -1).length", float64(2)},
		{"result.content.slice(-1)[0].type", "tool-call"},
		{"JSON.parse(call.input).call_id", "c9"},
		{"(call as { input: string }).toolName", "x"},
		{"(await call).toolName", "x"},
		{"result.content?.[1].url", "u1"},
	} {
		got, ok := evalPath(roots, aliases, tc.expr)
		if !ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s = %#v (%v), want %#v", tc.expr, got, ok, tc.want)
		}
	}
	// Outside an opted-in suite the extended forms are not evaluated.
	extendedAsserts = false
	if _, ok := evalPath(roots, aliases, "sources.length"); ok {
		t.Error("extended form evaluated without opting in")
	}
}

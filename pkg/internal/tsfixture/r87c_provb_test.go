package tsfixture

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// r87cprovbRoots is the input of TestR87cprovbOperatorForms; the expected
// values are node 24's results of the same expressions over it.
func r87cprovbRoots() map[string]interface{} {
	return map[string]interface{}{
		"result": map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "Hello TypeScript", "providerMetadata": map[string]interface{}{"openai": map[string]interface{}{"annotations": []interface{}{"a1"}}}},
				map[string]interface{}{"type": "source", "url": "u1"},
				map[string]interface{}{"type": "source", "url": "u1"},
				map[string]interface{}{"type": "source", "url": "u2"},
				map[string]interface{}{"type": "tool-call", "name": "search", "toolName": "x"},
				map[string]interface{}{"type": "reasoning-start", "id": "r1"},
				map[string]interface{}{"type": "text-delta", "id": "t1", "delta": "He"},
				map[string]interface{}{"type": "text-delta", "id": "t1", "delta": "llo"},
			},
			"tools":    []interface{}{map[string]interface{}{"name": "search"}, map[string]interface{}{"type": "code_interpreter"}},
			"input":    []interface{}{map[string]interface{}{"type": "message", "content": []interface{}{"c0"}}, "x"},
			"response": map[string]interface{}{"id": "r", "modelId": "m"},
		},
	}
}

// The operator forms TS assertions write (`new Set(...)`, `'k' in x ? a :
// b`, ||, includes, Array.isArray, ??, object literals, typed arrow
// parameters, casts split over lines) evaluate like JS.
func TestR87cprovbOperatorForms(t *testing.T) {
	EvalExtendedAsserts(t)
	roots := r87cprovbRoots()
	for _, tc := range []struct {
		expr string
		want interface{}
	}{
		{"new Set(result.content.map(p => p.url)).size", float64(3)},
		{"result.tools?.map(tool => ('name' in tool ? tool.name : tool.type)),", []interface{}{"search", "code_interpreter"}},
		{"'content' in result.input[0] ? result.input[0].content[0] : null,", "c0"},
		{"'missing' in result.response", false},
		{"result.content.flatMap(part =>part.type === 'reasoning-start' ||part.type === 'text-delta'? [part.id]: [],)", []interface{}{"r1", "t1", "t1"}},
		{"result.content\n  .filter(p => p.type === 'text-delta')\n  .map(p => p.delta)\n  .join(''),", "Hello"},
		{"result.content.filter(part => part.type.startsWith('reasoning-') || part.type.startsWith('text-')).map(part => part.type)", []interface{}{"reasoning-start", "text-delta", "text-delta"}},
		{"result.content.filter(event =>\n  ['source', 'tool-call'].includes(event.type),\n).length", float64(4)},
		{"result.content.some(part => part.type === 'text' && part.text.includes('TypeScript'))", true},
		{"Array.isArray(result.input)\n  ? result.input.map(item => item.type)\n  : undefined,", []interface{}{"message", nil}},
		{"result.content.flatMap(\n  part =>\n    (part.providerMetadata?.openai?.annotations as\n      | unknown[]\n      | null) ?? [],\n)", []interface{}{"a1"}},
		{"result.response.missing ?? 'dflt'", "dflt"},
		{"result.tools.map((t: any) => t.name)", []interface{}{"search", nil}},
		{"result.tools.map((tool: { name: string }) => tool.name)", []interface{}{"search", nil}},
		{"{\n  id: result.response?.id,\n  modelId: result.response?.modelId,\n}", map[string]interface{}{"id": "r", "modelId": "m"}},
	} {
		got, ok := evalPath(roots, nil, tc.expr)
		if !ok || !reflect.DeepEqual(jsonValue(got), tc.want) {
			t.Errorf("%s = %#v (%v), want %#v", tc.expr, got, ok, tc.want)
		}
	}
	// an alias that is an operator expression applies before the path
	aliases := map[string]string{"anns": "result.content[0].providerMetadata?.openai.annotations ?? []"}
	if got, ok := evalPath(roots, aliases, "anns.length"); !ok || got != float64(1) {
		t.Errorf("anns.length = %v (%v), want 1", got, ok)
	}
}

// toEqual over two Sets compares membership in any order (vitest equals);
// a Set built from an array keeps one of each equal primitive.
func TestR87cprovbSetEquality(t *testing.T) {
	EvalExtendedAsserts(t)
	roots := r87cprovbRoots()
	for _, tc := range []struct {
		arg  []interface{}
		pass bool
	}{
		{[]interface{}{"u2", nil, "u1"}, true},
		{[]interface{}{"u1", "u2"}, false},
		{[]interface{}{"u1", "u2", nil, "u3"}, false},
	} {
		a := Assertion{Expr: "new Set(result.content.map(p => p.url))", Matcher: "toEqual", Arg: map[string]interface{}{"__set": tc.arg}}
		msg, skipped := a.check(roots, nil)
		if skipped || (msg == "") != tc.pass {
			t.Errorf("toEqual(new Set(%v)): msg %q skipped %v, want pass %v", tc.arg, msg, skipped, tc.pass)
		}
	}
}

// A suite-scoped opt-in leaves a binary-wide EnableExtendedAsserts on when
// it ends (it used to clear it for every later suite).
func TestR87cprovbOptInRestoresPrevious(t *testing.T) {
	prevExt, prevSnap := extendedAsserts, evalPathSnapshots
	defer func() { extendedAsserts, evalPathSnapshots = prevExt, prevSnap }()
	extendedAsserts, evalPathSnapshots = true, true
	t.Run("suite", func(t *testing.T) {
		EvalExtendedAsserts(t)
		EvalPathSnapshots(t)
	})
	if !extendedAsserts || !evalPathSnapshots {
		t.Errorf("after a suite opt-in: extendedAsserts %v, evalPathSnapshots %v; want both still true", extendedAsserts, evalPathSnapshots)
	}
}

// The case server sends no Date header (the TS test server sends only the
// headers a response sets; providers read a Date header as the response
// timestamp), unless the case sets one.
func TestR87cprovbNoDateHeader(t *testing.T) {
	for _, tc := range []struct {
		resp *Response
		want string
	}{
		{&Response{Kind: "json", Body: []byte(`{}`)}, ""},
		{&Response{Kind: "chunks"}, ""},
		{&Response{Kind: "error", Status: 500, Text: "x"}, ""},
		{&Response{Kind: "json", Body: []byte(`{}`), Headers: map[string]string{"date": "Mon, 02 Jan 2006 15:04:05 GMT"}}, "Mon, 02 Jan 2006 15:04:05 GMT"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { tc.resp.serve(w) }))
		res, err := http.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		srv.Close()
		if got := res.Header.Get("Date"); got != tc.want {
			t.Errorf("%s response: Date %q, want %q", tc.resp.Kind, got, tc.want)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { notFound(w) }))
	defer srv.Close()
	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if got := res.Header.Get("Date"); got != "" {
		t.Errorf("not found: Date %q, want none", got)
	}
}

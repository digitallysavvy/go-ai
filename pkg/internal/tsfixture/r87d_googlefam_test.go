package tsfixture

import (
	"fmt"
	"strings"
	"testing"
)

// r87dgooglefamTB records what runAsserts logs and reports.
type r87dgooglefamTB struct {
	testing.TB
	logs, errs []string
}

func (r *r87dgooglefamTB) Helper() {}
func (r *r87dgooglefamTB) Logf(f string, a ...interface{}) {
	r.logs = append(r.logs, fmt.Sprintf(f, a...))
}
func (r *r87dgooglefamTB) Errorf(f string, a ...interface{}) {
	r.errs = append(r.errs, fmt.Sprintf(f, a...))
}
func (r *r87dgooglefamTB) Error(a ...interface{}) { r.errs = append(r.errs, fmt.Sprint(a...)) }

// TestR87dgooglefamRegisterPorted: a marked assertion the evaluator cannot
// evaluate is neither counted nor logged as not evaluated (logged as
// hand-ported with TSFIXTURE_SKIPPED); unmarked, it is; CheckPortedUsed
// reports marks that match nothing or whose assertion evaluates.
func TestR87dgooglefamRegisterPorted(t *testing.T) {
	t.Setenv("TSFIXTURE_SKIPPED", "1")
	c := Case{Test: "r87dgooglefam case", Asserts: []Assertion{
		{Expr: "Object.keys(r87dgooglefamUnknownRoot)", Matcher: "toEqual", Arg: []interface{}{"a"}},
		{Expr: "result.a", Matcher: "toBe", Arg: float64(1)},
	}}
	roots := map[string]interface{}{"result": map[string]interface{}{"a": float64(1)}}

	unmarked := &r87dgooglefamTB{TB: t}
	runAsserts(unmarked, c, roots)
	if got := strings.Join(unmarked.logs, "\n"); !strings.Contains(got, "not evaluated: expect(Object.keys(r87dgooglefamUnknownRoot)).toEqual") ||
		!strings.Contains(got, "1 of 2 plain assertions not evaluated") {
		t.Fatalf("unmarked logs = %q", got)
	}

	marks := map[string]string{
		PortedKey(c.Test, "Object.keys(r87dgooglefamUnknownRoot)", "toEqual"): "TestSomething",
		PortedKey(c.Test, "result.a", "toBe"):                                 "TestEvaluates",
		PortedKey("r87dgooglefam no such case", "x", "toBe"):                  "TestStale",
	}
	RegisterPorted(marks)
	defer func() {
		portedMu.Lock()
		for k := range marks {
			delete(portedAsserts, k)
			delete(portedSeen, k)
		}
		portedMu.Unlock()
	}()
	marked := &r87dgooglefamTB{TB: t}
	runAsserts(marked, c, roots)
	got := strings.Join(marked.logs, "\n")
	if strings.Contains(got, "not evaluated") {
		t.Errorf("marked assertion still counted: %q", got)
	}
	if !strings.Contains(got, "hand-ported: expect(Object.keys(r87dgooglefamUnknownRoot)).toEqual -> TestSomething") {
		t.Errorf("hand-ported log missing: %q", got)
	}
	if len(marked.errs) != 0 {
		t.Errorf("errors: %q", marked.errs)
	}

	check := &r87dgooglefamTB{TB: t}
	stale := CheckPortedUsed(check, marks)
	if len(stale) != 2 || len(check.errs) != 2 ||
		!strings.Contains(stale[0], "TestEvaluates") || !strings.Contains(stale[0], "not needed") ||
		!strings.Contains(stale[1], "TestStale") || !strings.Contains(stale[1], "matches no replayed assertion") {
		t.Errorf("stale = %q", stale)
	}
}

// TestR87dgooglefamJSFallback: an assertion evalPath cannot evaluate runs its
// JS program over the replayed roots (a loop-built local, Object.keys, the
// server's calls), tells undefined from null, checks an expectation inside a
// loop for every value, and stays not evaluated when the program throws.
func TestR87dgooglefamJSFallback(t *testing.T) {
	roots := map[string]interface{}{
		"result": map[string]interface{}{
			"stream": []interface{}{
				map[string]interface{}{"type": "text-delta", "delta": "Hel"},
				map[string]interface{}{"type": "text-delta", "delta": "lo"},
				map[string]interface{}{"type": "finish", "a": nil},
			},
		},
		"server": map[string]interface{}{"calls": []request{{body: map[string]interface{}{"b": float64(1), "a": "x"}, headers: map[string]interface{}{}}}},
	}
	const loop = `async (__result) => { return await (async () => {
		const { stream } = __result;
		const parts = await convertReadableStreamToArray(stream);
		let text = '';
		for (const p of parts) { if (p.type === 'text-delta') text += p.delta; }
		expect(text).toBe('ignored');
		return (text);
	})(); }`
	cases := []struct {
		name string
		a    Assertion
		fail bool
		skip bool
	}{
		{name: "loop-built local", a: Assertion{Expr: "text", Matcher: "toBe", Arg: "Hello", JS: loop}},
		{name: "loop-built local, wrong value", a: Assertion{Expr: "text", Matcher: "toBe", Arg: "Hell", JS: loop}, fail: true},
		{name: "Object.keys over a request body", a: Assertion{Expr: "Object.keys(body)", Matcher: "toEqual", Arg: []interface{}{"a", "b"},
			JS: `async () => { const body = await server.calls[0].requestBodyJson; return Object.keys(body).sort(); }`}},
		{name: "undefined is not null", a: Assertion{Expr: "x", Matcher: "toBeNull",
			JS: `async (r) => r.stream[2].missing`}, fail: true},
		{name: "null is defined", a: Assertion{Expr: "x", Matcher: "toBeDefined",
			JS: `async (r) => r.stream[2].a`}},
		{name: "undefined toBeUndefined", a: Assertion{Expr: "x", Matcher: "toBeUndefined",
			JS: `async (r) => r.stream[2].missing`}},
		{name: "null is not undefined", a: Assertion{Expr: "x", Matcher: "toBeUndefined",
			JS: `async (r) => r.stream[2].a`}, fail: true},
		{name: "toBe(undefined) on null", a: Assertion{Expr: "x", Matcher: "toBe", Arg: map[string]interface{}{"__undef": true},
			JS: `async (r) => r.stream[2].a`}, fail: true},
		{name: "every value in a loop", a: Assertion{Expr: "p.type", Matcher: "toBe", Arg: "text-delta",
			JS: `async (r) => { const __each = []; for (const p of r.stream.slice(0, 2)) { __each.push(p.type); } return { __each }; }`}},
		{name: "one value in a loop fails", a: Assertion{Expr: "p.type", Matcher: "toBe", Arg: "text-delta",
			JS: `async (r) => { const __each = []; for (const p of r.stream) { __each.push(p.type); } return { __each }; }`}, fail: true},
		{name: "a throwing program is not evaluated", a: Assertion{Expr: "x", Matcher: "toBe", Arg: float64(1),
			JS: `async () => { await model.doGenerate({}); return 1; }`}, skip: true},
		{name: "no program", a: Assertion{Expr: "Object.keys(result)", Matcher: "toEqual", Arg: []interface{}{}}, skip: true},
		{name: "Uint8Array and Set encode as the extractor does", a: Assertion{Expr: "x", Matcher: "toEqual",
			Arg: map[string]interface{}{"b": map[string]interface{}{"__bytes": "AQID"}, "s": map[string]interface{}{"__set": []interface{}{"a"}}},
			JS:  `async () => ({ b: new Uint8Array([1, 2, 3]), s: new Set(['a']) })`}},
	}
	for _, tc := range cases {
		msg, skipped := tc.a.check(roots, nil)
		if skipped != tc.skip || (msg != "") != tc.fail {
			t.Errorf("%s: msg %q, skipped %v; want fail %v, skip %v", tc.name, msg, skipped, tc.fail, tc.skip)
		}
	}
}

// TestR87dgooglefamNotApplicableAndHelpers: an n/a mark is neither evaluated
// nor counted (and logged with its reason); SpyRoot, FindAssertion and
// MatchAssertion serve hand-ports.
func TestR87dgooglefamNotApplicableAndHelpers(t *testing.T) {
	t.Setenv("TSFIXTURE_SKIPPED", "1")
	c := Case{Test: "r87dgooglefam n/a case", Asserts: []Assertion{{Expr: "result.a", Matcher: "toBe", Arg: float64(2)}}}
	marks := map[string]string{PortedKey(c.Test, "result.a", "toBe"): "a Go map has no key order"}
	RegisterNotApplicable(marks)
	defer func() {
		portedMu.Lock()
		for k := range marks {
			delete(notApplicable, k)
			delete(portedSeen, k)
		}
		portedMu.Unlock()
	}()
	rec := &r87dgooglefamTB{TB: t}
	runAsserts(rec, c, map[string]interface{}{"result": map[string]interface{}{"a": float64(1)}})
	if len(rec.errs) != 0 || len(rec.logs) != 1 || !strings.Contains(rec.logs[0], "n/a: expect(result.a).toBe: a Go map has no key order") {
		t.Errorf("logs %q, errs %q", rec.logs, rec.errs)
	}
	if stale := CheckPortedUsed(nil, marks); len(stale) != 0 {
		t.Errorf("stale = %q", stale)
	}

	spy := SpyRoot([]interface{}{map[string]interface{}{"message": "m1"}})
	got, ok := evalPath(map[string]interface{}{"onWarning": spy}, nil, "onWarning.mock.calls[0][0].message")
	if !ok || got != "m1" {
		t.Errorf("spy path = %v, %v", got, ok)
	}
	_, a, found := FindAssertion([]Case{c}, c.Test, "result.a", "toBe")
	if !found {
		t.Fatal("FindAssertion")
	}
	if msg, ok := MatchAssertion(a, float64(2)); !ok || msg != "" {
		t.Errorf("MatchAssertion(2) = %q, %v", msg, ok)
	}
	if msg, ok := MatchAssertion(a, float64(3)); !ok || msg == "" {
		t.Errorf("MatchAssertion(3) = %q, %v", msg, ok)
	}
}

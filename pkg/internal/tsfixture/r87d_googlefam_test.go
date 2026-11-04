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

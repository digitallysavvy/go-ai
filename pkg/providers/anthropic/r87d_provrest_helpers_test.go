package anthropic

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
)

// Round 87d hand-port helpers (R87D_TSFIXTURE closure rule): the committed
// case's TS input and expected value, and TS-style comparisons.

// r87dprovrestJSON is v as encoding/json decodes it (the TS JSON value).
func r87dprovrestJSON(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// r87dprovrestEqual fails unless got equals want as JSON values (toEqual /
// toStrictEqual); a TS undefined in want ({"__undef": true}) matches an
// absent member or a Go nil, which is how Go renders undefined.
func r87dprovrestEqual(t *testing.T, what string, want, got interface{}) {
	t.Helper()
	w, g := r87dprovrestJSON(t, want), r87dprovrestJSON(t, got)
	if !r87dprovrestMatch(w, g, false) {
		wb, _ := json.Marshal(w)
		gb, _ := json.Marshal(g)
		t.Errorf("%s:\n TS %s\n Go %s", what, wb, gb)
	}
}

func r87dprovrestIsUndef(v interface{}) bool {
	m, ok := v.(map[string]interface{})
	return ok && len(m) == 1 && m["__undef"] == true
}

func r87dprovrestMatch(want, got interface{}, partial bool) bool {
	if r87dprovrestIsUndef(want) {
		return got == nil
	}
	switch w := want.(type) {
	case map[string]interface{}:
		g, ok := got.(map[string]interface{})
		if !ok {
			return false
		}
		for k, e := range w {
			if !r87dprovrestMatch(e, g[k], partial) {
				return false
			}
		}
		if !partial {
			for k, e := range g {
				if _, ok := w[k]; !ok && e != nil {
					return false
				}
				if _, ok := w[k]; !ok && e == nil {
					return false // a Go null where TS has no member
				}
			}
		}
		return true
	case []interface{}:
		g, ok := got.([]interface{})
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !r87dprovrestMatch(w[i], g[i], partial) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(want, got)
}

// r87dprovrestFnArgs are the evaluated TS arguments of the function-level case
// named test of file.
func r87dprovrestFnArgs(t *testing.T, file, test string) []interface{} {
	t.Helper()
	for _, c := range tsfixture.LoadCases(t, file) {
		if c.Test == test {
			return c.FnArgs
		}
	}
	t.Fatalf("%s: case %q not found", file, test)
	return nil
}

// r87dprovrestArg is the TS expected value (as the case file records it) of
// the assertion expr of the nth (0-based) case named test of file.
func r87dprovrestArg(t *testing.T, file, test string, nth int, expr string) interface{} {
	t.Helper()
	seen := 0
	for _, c := range tsfixture.LoadCases(t, file) {
		if c.Test != test {
			continue
		}
		if seen == nth {
			for _, a := range c.Asserts {
				if a.Expr == expr {
					return a.Arg
				}
			}
		}
		seen++
	}
	t.Fatalf("%s: assertion %q of %q #%d not found", file, expr, test, nth)
	return nil
}

// The package's hand-port marks (r87dprovrestPorted, in
// r87d_provrest_ported_test.go) are registered for every suite.
func init() { tsfixture.RegisterPorted(r87dprovrestPorted) }

// TestMain fails an unfiltered run whose hand-port or n/a marks are stale.
func TestMain(m *testing.M) {
	os.Exit(tsfixture.PortedMain(m, r87dprovrestPorted, r87dprovrestNA, r88provaPorted))
}

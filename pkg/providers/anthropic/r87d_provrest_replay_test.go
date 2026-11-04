package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Round 87d hand ports (R87D_TSFIXTURE closure rule): r87dprovrestReplay runs
// one committed TS case through tsfixture.Run (the same canned response and
// call options the replay uses) and hands back what the Go model produced,
// so a hand-ported test can compute the TS expression the evaluator does not
// read, over the same input, and compare it with the TS expected value.

type r87dprovrestReq struct {
	body    interface{}
	headers http.Header
}

type r87dprovrestRun struct {
	gen    *types.GenerateResult
	genErr error
	chunks []*provider.StreamChunk
	parts  []interface{} // tsfixture.StreamToTS(chunks)
	mu     sync.Mutex
	reqs   []r87dprovrestReq
}

func (r *r87dprovrestRun) req(t *testing.T, i int) r87dprovrestReq {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= len(r.reqs) {
		t.Fatalf("request %d not sent (%d requests)", i, len(r.reqs))
	}
	return r.reqs[i]
}

type r87dprovrestModel struct {
	provider.LanguageModel
	run *r87dprovrestRun
}

func (m r87dprovrestModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	res, err := m.LanguageModel.DoGenerate(ctx, opts)
	m.run.gen, m.run.genErr = res, err
	return res, err
}

func (m r87dprovrestModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	s, err := m.LanguageModel.DoStream(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &r87dprovrestStream{TextStream: s, run: m.run}, nil
}

type r87dprovrestStream struct {
	provider.TextStream
	run *r87dprovrestRun
}

func (s *r87dprovrestStream) Next() (*provider.StreamChunk, error) {
	c, err := s.TextStream.Next()
	if err == nil && c != nil {
		s.run.chunks = append(s.run.chunks, c)
	}
	return c, err
}

// r87dprovrestReplay replays the nth (0-based) case named test of file; prep
// (optional) applies the rewrite the package's replay suite applies.
func r87dprovrestReplay(t *testing.T, file, test string, nth int, prep func(*tsfixture.Case), mk func(base, id string) provider.LanguageModel) *r87dprovrestRun {
	t.Helper()
	var picked []tsfixture.Case
	seen := 0
	for _, c := range tsfixture.LoadCases(t, file) {
		if c.Test != test {
			continue
		}
		if seen == nth {
			c.Asserts, c.Exps = nil, nil // the replay suites check those
			if prep != nil {
				prep(&c)
			}
			picked = append(picked, c)
		}
		seen++
	}
	if len(picked) != 1 {
		t.Fatalf("%s: case %q #%d not found", file, test, nth)
	}
	run := &r87dprovrestRun{}
	tsfixture.Run(t, picked, func(base, id string) provider.LanguageModel {
		target, err := url.Parse(base)
		if err != nil {
			t.Fatal(err)
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			var body interface{}
			_ = json.Unmarshal(b, &body)
			run.mu.Lock()
			run.reqs = append(run.reqs, r87dprovrestReq{body: body, headers: r.Header.Clone()})
			run.mu.Unlock()
			r.Body = io.NopCloser(bytes.NewReader(b))
			proxy.ServeHTTP(w, r)
		}))
		t.Cleanup(srv.Close)
		return r87dprovrestModel{LanguageModel: mk(srv.URL, id), run: run}
	}, nil)
	if run.chunks != nil {
		run.parts = tsfixture.StreamToTS(run.chunks)
	}
	return run
}

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

// r87dprovrestMatchObject is r87dprovrestEqual for toMatchObject (object
// members of want are a subset).
func r87dprovrestMatchObject(t *testing.T, what string, want, got interface{}) {
	t.Helper()
	w, g := r87dprovrestJSON(t, want), r87dprovrestJSON(t, got)
	if !r87dprovrestMatch(w, g, true) {
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

// TestMain fails a full run whose hand-port marks are stale (a mark that no
// replayed assertion reaches, or whose assertion now evaluates).
func TestMain(m *testing.M) {
	code := m.Run()
	if code == 0 && r87dprovrestFullRun() {
		if stale := tsfixture.CheckPortedUsed(nil, r87dprovrestPorted); len(stale) > 0 {
			for _, s := range stale {
				fmt.Fprintln(os.Stderr, s)
			}
			code = 1
		}
	}
	os.Exit(code)
}

func r87dprovrestFullRun() bool {
	for _, name := range []string{"test.run", "test.skip"} {
		if f := flag.Lookup(name); f != nil && f.Value.String() != "" {
			return false
		}
	}
	return true
}

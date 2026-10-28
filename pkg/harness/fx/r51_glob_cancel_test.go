package fx

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// compileWithin runs compileGlobContext under a context canceled after 50 ms
// and fails unless it returns the context's error within limit.
func compileWithin(t *testing.T, pattern string, limit time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	type result struct {
		g   *globPattern
		err error
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		g, err := compileGlobContext(ctx, pattern, false, false)
		done <- result{g, err}
	}()
	select {
	case r := <-done:
		if !errors.Is(r.err, context.DeadlineExceeded) || r.g != nil {
			t.Errorf("%.12q...: compile = %v, %v; want context.DeadlineExceeded", pattern, r.g, r.err)
		}
		if d := time.Since(start); d > limit {
			t.Errorf("%.12q...: returned after %v", pattern, d)
		}
	case <-time.After(limit):
		t.Fatalf("%.12q...: compile still running %v after a 50ms deadline", pattern, limit)
	}
}

// The fx glob tool's pattern compile stops when the call's context ends: a
// `{a,b}` x17 pattern (about 40 s of CPU in both Node and Go, in minimatch's
// quadratic second-phase merge of its 100000 expansions) returns the
// context's error promptly instead of finishing the compile.
func TestR51GlobCompileStopsOnCancel(t *testing.T) {
	sb := newFakeSandbox()
	sb.handle = func(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
		if strings.Contains(opts.Command, "realpath") {
			return providerutils.SandboxRunResult{Stdout: b64("/workspace\x00")}, nil
		}
		return providerutils.SandboxRunResult{Stdout: "/workspace/a.txt\x00"}, nil
	}
	r := newRemoteOps(sb, "/workspace")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	input, _ := json.Marshal(map[string]string{"pattern": strings.Repeat("{a,b}", 17)})
	done := make(chan error, 1)
	go func() {
		_, err := r.execute(ctx, "glob", input)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("glob = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("glob compile ignored the context")
	}
	compileWithin(t, strings.Repeat("{a,b}", 17), 5*time.Second)
}

// Braces nested 20000 deep (Node throws "Maximum call stack size exceeded";
// Go expands them for tens of seconds) stop at the deadline too, in the
// expansion as well as in the preprocessing.
func TestR51GlobCompileNestedBracesStopOnCancel(t *testing.T) {
	for _, p := range []string{
		strings.Repeat("{,", 20000) + strings.Repeat("}", 20000),
		strings.Repeat("{a,", 15000) + strings.Repeat("}", 15000),
		strings.Repeat("{", 20000) + "a,b" + strings.Repeat("}", 20000),
	} {
		compileWithin(t, p, 5*time.Second)
	}
}

// A context that does not end leaves the compile unchanged, and a context
// that already ended fails before any work.
func TestR51GlobCompileContextSameResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, p := range []string{strings.Repeat("{a,b}", 10), "{**/x,x/y}", "a/./b/../{c,d}/**/../*.go", "{1..20..3}/{x,y,}", strings.Repeat("{a,", 50) + strings.Repeat("}", 50)} {
		want, err := compileGlob(p, true, false)
		if err != nil {
			t.Fatal(err)
		}
		got, err := compileGlobContext(ctx, p, true, false)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%q: context compile differs (%v)", p, err)
		}
	}
	cancel()
	if g, err := compileGlobContext(ctx, "*.go", false, false); !errors.Is(err, context.Canceled) || g != nil {
		t.Errorf("canceled compile = %v, %v", g, err)
	}
}

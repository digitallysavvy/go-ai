package qjs

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	wsp1 "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// These tests cover the go-ai shared-runtime patch (see sharedRuntime in
// runtime.go): every qjs.New instantiates its own anonymous WASI, env and
// guest instances in one process-wide wazero Runtime. Nothing may leak
// between runs.

func sandboxOption(ctx context.Context) Option {
	return Option{
		Context:            ctx,
		CloseOnContextDone: true,
		NoFSMount:          true,
		DisableModules:     true,
		Stdout:             io.Discard,
		Stderr:             io.Discard,
	}
}

func evalString(t *testing.T, rt *Runtime, code string) string {
	t.Helper()
	v, err := rt.Eval("test.js", Code(code))
	if err != nil {
		t.Fatalf("eval %q: %v", code, err)
	}
	defer v.Free()
	return v.String()
}

func TestSharedRuntime_GlobalsAndMemoryDoNotLeakBetweenRuns(t *testing.T) {
	a, err := New(sandboxOption(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	defer closeRuntimeForTest(a)
	evalString(t, a, `globalThis.leak = "from-a"; "ok"`)
	addr := uint32(a.Malloc(16))
	a.Mem().MustWrite(addr, []byte("secret-from-run-a"[:16]))

	b, err := New(sandboxOption(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	defer closeRuntimeForTest(b)
	if got := evalString(t, b, `typeof globalThis.leak`); got != "undefined" {
		t.Fatalf("run b sees run a's global: typeof leak = %q", got)
	}
	if a.module.Memory() == b.module.Memory() {
		t.Fatal("runs share one linear memory")
	}
	if got, ok := b.module.Memory().Read(addr, 16); ok && string(got) == "secret-from-run-" {
		t.Fatalf("run b reads run a's memory at %d", addr)
	}
}

// Each run binds its own jsFunctionProxy (its own ProxyRegistry). Proxy ids
// start at the same value in every registry, so a run resolved to another
// run's env instance would call the other run's Go function.
func TestSharedRuntime_HostBindingsArePerRun(t *testing.T) {
	const runs = 6
	var wg sync.WaitGroup
	errs := make(chan error, runs)
	for i := 0; i < runs; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rt, err := New(sandboxOption(context.Background()))
			if err != nil {
				errs <- err
				return
			}
			defer closeRuntimeForTest(rt)
			tag := fmt.Sprintf("run-%d", i)
			rt.Context().SetFunc("who", func(this *This) (*Value, error) {
				return this.Context().NewString(tag), nil
			})
			for j := 0; j < 50; j++ {
				v, err := rt.Eval("who.js", Code(`who()`))
				if err != nil {
					errs <- err
					return
				}
				got := v.String()
				v.Free()
				if got != tag {
					errs <- fmt.Errorf("run %d called another run's host function: got %q", i, got)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestSharedRuntime_NothingRegisteredByName(t *testing.T) {
	rt, err := New(sandboxOption(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	defer closeRuntimeForTest(rt)
	compilationMutex.Lock()
	sr := shared
	compilationMutex.Unlock()
	if sr == nil {
		t.Fatal("no shared runtime")
	}
	// An import the resolver does not bind falls back to the store by name;
	// with nothing named there it fails instead of reaching another run.
	for _, name := range []string{"env", wsp1.ModuleName, ""} {
		if m := sr.rt.Module(name); m != nil {
			t.Fatalf("shared runtime has a module registered as %q", name)
		}
	}
}

func TestSharedRuntime_CloseReleasesPerRunInstances(t *testing.T) {
	for _, disable := range []bool{false, true} {
		opt := sandboxOption(context.Background())
		opt.DisableBuildCache = disable
		rt, err := New(opt)
		if err != nil {
			t.Fatal(err)
		}
		if got := evalString(t, rt, `"x" + 1`); got != "x1" {
			t.Fatalf("eval = %q", got)
		}
		mods := []interface{ IsClosed() bool }{rt.module, rt.envModule, rt.wasiModule}
		if disable != (rt.ownedRuntime != nil) {
			t.Fatalf("DisableBuildCache=%v: ownedRuntime set = %v", disable, rt.ownedRuntime != nil)
		}
		rt.Close()
		for i, m := range mods {
			if !m.IsClosed() {
				t.Fatalf("DisableBuildCache=%v: instance %d still open after Close", disable, i)
			}
		}
	}
}

// Canceling one run's context closes only that run's guest.
func TestSharedRuntime_CancelOneRunLeavesOthers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	victim, err := New(sandboxOption(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer closeRuntimeForTest(victim)
	other, err := New(sandboxOption(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	defer closeRuntimeForTest(other)

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("%v", r)
			}
		}()
		_, err := victim.Eval("spin.js", Code(`for (;;) {}`))
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled run returned no error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("canceled run did not stop")
	}
	if got := evalString(t, other, `[1,2,3].map(x => x * 2).join(",")`); got != "2,4,6" {
		t.Fatalf("other run broken after cancel: %q", got)
	}
	third, err := New(sandboxOption(context.Background()))
	if err != nil {
		t.Fatalf("New after a canceled run: %v", err)
	}
	defer closeRuntimeForTest(third)
	if got := evalString(t, third, `typeof globalThis.leak`); got != "undefined" {
		t.Fatalf("typeof leak = %q", got)
	}
}

type countingListenerFactory struct{ calls *atomic.Int64 }

func (f countingListenerFactory) NewFunctionListener(api.FunctionDefinition) experimental.FunctionListener {
	return countingListener(f)
}

type countingListener struct{ calls *atomic.Int64 }

func (l countingListener) Before(context.Context, api.Module, api.FunctionDefinition, []uint64, experimental.StackIterator) {
	l.calls.Add(1)
}
func (countingListener) After(context.Context, api.Module, api.FunctionDefinition, []uint64) {}
func (countingListener) Abort(context.Context, api.Module, api.FunctionDefinition, error)    {}

// wazero reads a function listener factory from the compile ctx. The shared
// runtime's compiled code serves every later run, so a listener in the ctx of
// the run that happened to compile it must not observe other runs.
func TestSharedRuntime_CompileCtxValuesDoNotReachOtherRuns(t *testing.T) {
	compilationMutex.Lock()
	prev, prevKey := shared, sharedKey
	shared = nil
	compilationMutex.Unlock()
	defer func() {
		compilationMutex.Lock()
		shared, sharedKey = prev, prevKey
		compilationMutex.Unlock()
	}()

	var calls atomic.Int64
	spyCtx := experimental.WithFunctionListenerFactory(context.Background(), countingListenerFactory{&calls})
	first, err := New(sandboxOption(spyCtx))
	if err != nil {
		t.Fatal(err)
	}
	closeRuntimeForTest(first)
	calls.Store(0)

	other, err := New(sandboxOption(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	defer closeRuntimeForTest(other)
	if got := evalString(t, other, `"private:" + (6 * 7)`); got != "private:42" {
		t.Fatalf("eval = %q", got)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("a listener from the compiling run's ctx observed %d calls of another run", n)
	}
}

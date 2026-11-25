package libfx

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

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

// The host is shared by every later engine, so a value in the ctx of the
// engine that happened to compile it (wazero reads a function listener factory
// at compile time) must not observe other engines' guest calls.
func TestSharedHostCompileCtxValuesDoNotReachOtherEngines(t *testing.T) {
	src := requireWasm(t)
	hostsMu.Lock()
	prev := hosts
	hosts = map[hostKey]*host{}
	hostsMu.Unlock()
	defer func() {
		hostsMu.Lock()
		for _, h := range hosts {
			_ = h.rt.Close(context.Background())
		}
		hosts = prev
		hostsMu.Unlock()
	}()

	var calls atomic.Int64
	spyCtx := experimental.WithFunctionListenerFactory(context.Background(), countingListenerFactory{&calls})
	first, err := Start(spyCtx, Options{Wasm: src, APIKey: "test-key", Model: "anthropic/claude-haiku-4.5", Stderr: testWriter{t}})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls.Store(0)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	other, err := Start(ctx, Options{Wasm: src, APIKey: "test-key", Model: "anthropic/claude-haiku-4.5", Stderr: testWriter{t}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Close(context.Background()) }()
	if _, err := other.Request(ctx, "initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("a listener from the compiling engine's ctx observed %d calls of another engine", n)
	}
}

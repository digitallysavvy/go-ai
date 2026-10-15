package codemode

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Hostile scripts must neither crash the host process, nor outlive the
// configured timeout (plus the documented grace), nor grow host memory
// without bound. QuickJS's own stack check does not fire in the wasm build,
// so every case below ends as a JS error, an engine trap, a memory-limit
// error or a timeout; none may take the process down.
var exhaustionCases = []struct {
	name string
	js   string
}{
	{"direct recursion", "function f(n){ return f(n+1)+1 } return f(0);"},
	{"mutual recursion", "function a(n){ return b(n)+1 } function b(n){ return a(n)+1 } return a(0);"},
	{"recursion through getter", "const o = { get x(){ return this.x } }; return o.x;"},
	{"recursion through proxy get", "const p = new Proxy({}, { get(t, k, r){ return r[k] } }); return p.foo;"},
	{"recursion through proxy has", "const p = new Proxy({}, { has(t, k){ return k in p } }); return 'x' in p;"},
	{"recursion through toString", "const o = { toString(){ return '' + o } }; return '' + o;"},
	{"recursion through valueOf", "const o = { valueOf(){ return o + 1 } }; return o + 1;"},
	{"recursion through toJSON", "const o = { toJSON(){ return JSON.stringify(o) } }; return JSON.stringify(o);"},
	{"recursion through array sort comparator", "function cmp(a, b){ return [2,1].sort(cmp) } return [2,1].sort(cmp);"},
	{"recursion through Array.map", "function g(){ return [1].map(g) } return g();"},
	{"recursion through async", "async function f(){ return await f() } return await f();"},
	{"recursion through constructor", "class A { constructor(){ new A() } } new A(); return 1;"},
	{"recursion through Symbol.toPrimitive", "const o = { [Symbol.toPrimitive](){ return +o } }; return +o;"},
	{"recursion through getter in prototype chain", "const o = Object.create({ get y(){ return o.y } }); return o.y;"},
	{"deeply nested JSON.parse", "return JSON.parse('['.repeat(2000000) + ']'.repeat(2000000));"},
	{"deeply nested array stringify", "let a = []; for (let i = 0; i < 1000000; i++) a = [a]; return JSON.stringify(a);"},
	{"deeply nested array flat", "let a = []; for (let i = 0; i < 1000000; i++) a = [a]; return a.flat(Infinity).length;"},
	{"deeply nested toString join", "let a = []; for (let i = 0; i < 1000000; i++) a = [a]; return String(a);"},
	{"huge Array length", "const a = new Array(4294967295).fill(0); return a.length;"},
	{"huge Array.from length", "return Array.from({length: 1e9}).length;"},
	{"unbounded push", "const a = []; while (true) a.push(0);"},
	{"huge string repeat", "return 'x'.repeat(2 ** 30).length;"},
	{"string doubling", "let s = 'x'; while (true) s += s;"},
	{"huge padStart", "return 'x'.padStart(2 ** 30).length;"},
	{"unbounded object growth", "const o = {}; for (let i = 0; ; i++) o['k' + i] = i;"},
	{"huge typed array", "return new Uint8Array(2 ** 31).length;"},
	{"huge ArrayBuffer", "return new ArrayBuffer(2 ** 32 - 1).byteLength;"},
	{"unbounded Map growth", "const m = new Map(); for (let i = 0; ; i++) m.set(i, i);"},
	{"unbounded closure growth", "const fs = []; for (let i = 0; ; i++) fs.push(() => i);"},
	{"huge join", "return new Array(2 ** 28).join('abcdefgh').length;"},
	{"huge regex input", "return 'a'.repeat(2 ** 26).replace(/a/g, 'bb').length;"},
}

func TestResourceExhaustionCannotCrashHangOrExhaustHost(t *testing.T) {
	const timeoutMs = 3000
	// A cold wasm compile (slow under -race) is not part of what the timeout
	// bounds, so pay for it before timing the cases.
	if err := warmUp(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range exhaustionCases {
		t.Run(tc.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			start := time.Now()
			got, err := RunCodeMode(context.Background(), RunInput{
				JS:      tc.js,
				Options: &Options{ExecutionPolicy: &ExecutionPolicy{TimeoutMs: timeoutMs}},
			})
			elapsed := time.Since(start)
			runtime.GC()
			runtime.ReadMemStats(&after)
			grown := int64(after.HeapInuse) - int64(before.HeapInuse)
			t.Logf("elapsed=%v heapGrowth=%dMiB err=%.120v got=%.60v", elapsed, grown>>20, err, got)
			if limit := time.Duration(timeoutMs)*time.Millisecond + 2*time.Second; elapsed > limit {
				t.Fatalf("ran for %v, past the %dms timeout (+2s)", elapsed, timeoutMs)
			}
			if grown > 512<<20 {
				t.Fatalf("host heap grew by %d MiB", grown>>20)
			}
		})
	}
}

// Engine traps from many sandboxes at once, with a host tool call per level
// of recursion, must stay confined to their own invocations.
func TestConcurrentRunawayRecursionWithHostCallsIsConfined(t *testing.T) {
	if err := warmUp(); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	tools := ToolSet{"ping": {
		Name:       "ping",
		Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			calls.Add(1)
			return 1, nil
		},
	}}
	policy := &Options{ExecutionPolicy: &ExecutionPolicy{TimeoutMs: 5000}}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			js := "function f(n){ return f(n+1)+1 } return f(0);"
			if i%2 == 1 {
				// recursion that re-enters the host through a getter calling a tool
				js = "const o = { get x(){ tools.ping({}); return this.x } }; return o.x;"
			}
			_, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools, Options: policy})
			if err == nil {
				t.Errorf("run %d: expected an error", i)
			}
		}(i)
	}
	wg.Wait()
	// A sandbox after the traps still works.
	got, err := RunCodeMode(context.Background(), RunInput{JS: "return 40 + 2;"})
	if err != nil || got != float64(42) {
		t.Fatalf("after traps: got %#v, err %v", got, err)
	}
}

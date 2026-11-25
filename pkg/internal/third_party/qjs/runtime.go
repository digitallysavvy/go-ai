package qjs

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	wsp1 "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:embed qjs.wasm
var wasmBytes []byte

// sharedRuntime is one long-lived wazero Runtime that compiled qjs.wasm and
// the WASI host module once. Every qjs.New instantiates its own anonymous
// WASI, env and guest module instances in it, so each run still has its own
// linear memory, globals, sys context (stdio, clock) and jsFunctionProxy
// binding, while decode, validation and compilation of the 1.28 MB module
// are paid once per process.
//
// go-ai patch (wazero v1.12.0): a CompiledModule may only be instantiated by
// the Runtime that compiled it, because since v1.12.0 wazero matches
// imported function signatures by per-Runtime type IDs. Upstream qjs
// compiled in a warm-up Runtime and instantiated in a fresh Runtime per
// call, which fails with "signature mismatch" on v1.12.0. Compiling in each
// Runtime works but costs a full decode and validation (~10 ms) per run, so
// the runtime is shared instead and the guest's imports are bound to its own
// per-run host instances with experimental.WithImportResolver. Nothing is
// ever registered by name in the shared Runtime, so an import the resolver
// does not bind fails to resolve instead of reaching another run's module.
type sharedRuntime struct {
	rt   wazero.Runtime
	qjs  wazero.CompiledModule
	wasi wazero.CompiledModule
}

type sharedRuntimeKey struct {
	// bytesHash is 0 for the embedded qjs.wasm, so the common path never
	// hashes 1.28 MB per run.
	bytesHash          uint64
	closeOnContextDone bool
	cacheDir           string
}

var (
	shared           *sharedRuntime
	sharedKey        sharedRuntimeKey
	compilationMutex sync.Mutex
)

// Runtime wraps a QuickJS WebAssembly runtime with memory management.
type Runtime struct {
	// ownedRuntime is set only with DisableBuildCache: a private wazero
	// Runtime closed with this one. Otherwise the instances below live in
	// the process-wide shared runtime.
	ownedRuntime wazero.Runtime
	wasiModule   api.Module
	envCompiled  wazero.CompiledModule
	envModule    api.Module
	module       api.Module
	malloc       api.Function
	free         api.Function
	mem          *Mem
	option       Option
	handle       *Handle
	context      *Context
	registry     *ProxyRegistry
}

// newRuntimeConfig returns the wazero config for a qjs Runtime.
func newRuntimeConfig(closeOnContextDone bool, cacheDir string) (wazero.RuntimeConfig, error) {
	cfg := wazero.NewRuntimeConfig().WithCloseOnContextDone(closeOnContextDone)
	if cacheDir != "" {
		cache, err := wazero.NewCompilationCacheWithDir(cacheDir)
		if err != nil {
			return nil, fmt.Errorf("failed to create compilation cache with dir %s: %w", cacheDir, err)
		}
		cfg = cfg.WithCompilationCache(cache)
	}
	return cfg, nil
}

// compileInto compiles qjs.wasm and the WASI host module in wrt.
func compileInto(ctx context.Context, wrt wazero.Runtime, qjsBytes []byte) (*sharedRuntime, error) {
	compiled, err := wrt.CompileModule(ctx, qjsBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to compile qjs module: %w", err)
	}
	wasi, err := wsp1.NewBuilder(wrt).Compile(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to compile WASI: %w", err)
	}
	return &sharedRuntime{rt: wrt, qjs: compiled, wasi: wasi}, nil
}

// sharedRuntimeFor returns the process-wide runtime for these options,
// compiling on first use or when the module bytes or runtime config change.
// A replaced shared runtime is not closed: runs still using it keep working
// and it is collected once they are closed.
func sharedRuntimeFor(closeOnContextDone bool, cacheDir string, qjsBytes []byte) (*sharedRuntime, error) {
	compilationMutex.Lock()
	defer compilationMutex.Unlock()

	key := sharedRuntimeKey{closeOnContextDone: closeOnContextDone, cacheDir: cacheDir}
	if !isEmbedded(qjsBytes) {
		key.bytesHash = hashBytes(qjsBytes) | 1 // never 0
	}
	if shared != nil && sharedKey == key {
		return shared, nil
	}
	cfg, err := newRuntimeConfig(closeOnContextDone, cacheDir)
	if err != nil {
		return nil, err
	}
	// The runtime outlives the run that first creates it, and its compiled
	// code serves every later run, so it is built from a bare context: a
	// value in that run's ctx (wazero reads an experimental function
	// listener factory at compile time) must not reach other runs.
	bare := context.Background()
	wrt := wazero.NewRuntimeWithConfig(bare, cfg)
	s, err := compileInto(bare, wrt, qjsBytes)
	if err != nil {
		_ = wrt.Close(bare)
		return nil, err
	}
	shared, sharedKey = s, key
	return s, nil
}

// isEmbedded reports whether b is the embedded qjs.wasm slice itself.
func isEmbedded(b []byte) bool {
	return len(b) == len(wasmBytes) && len(b) > 0 && &b[0] == &wasmBytes[0]
}

// qjsBytesFor returns the override module bytes when given, else the
// embedded qjs.wasm.
func qjsBytesFor(quickjsWasmBytes ...[]byte) []byte {
	if len(quickjsWasmBytes) > 0 && len(quickjsWasmBytes[0]) > 0 {
		return quickjsWasmBytes[0]
	}
	return wasmBytes
}

// New creates a QuickJS runtime with optional configuration.
func New(options ...Option) (runtime *Runtime, err error) {
	defer func() {
		rerr := AnyToError(recover())
		if rerr != nil {
			runtime = nil
			err = fmt.Errorf("failed to create QJS runtime: %w", rerr)
		}
	}()

	proxyRegistry := NewProxyRegistry()

	option, err := getRuntimeOption(proxyRegistry, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to get runtime options: %w", err)
	}

	runtime = &Runtime{
		option:   option,
		context:  &Context{Context: option.Context},
		registry: proxyRegistry,
	}

	qjsBytes := qjsBytesFor(option.QuickJSWasmBytes)
	var sr *sharedRuntime
	if option.DisableBuildCache {
		// A private Runtime, closed with this one, compiled from scratch.
		cfg, cerr := newRuntimeConfig(option.CloseOnContextDone, option.CacheDir)
		if cerr != nil {
			return nil, cerr
		}
		wrt := wazero.NewRuntimeWithConfig(option.Context, cfg)
		if sr, err = compileInto(option.Context, wrt, qjsBytes); err != nil {
			_ = wrt.Close(option.Context)
			return nil, err
		}
		runtime.ownedRuntime = wrt
	} else if sr, err = sharedRuntimeFor(option.CloseOnContextDone, option.CacheDir, qjsBytes); err != nil {
		return nil, fmt.Errorf("failed to create global compiled module: %w", err)
	}
	// Release whatever was instantiated if a later step fails or panics
	// (initializeRuntime panics on a failed call; the recover above runs
	// after this).
	created := runtime
	succeeded := false
	defer func() {
		if !succeeded {
			created.closeInstances()
		}
	}()

	anonymous := wazero.NewModuleConfig().WithName("")
	if runtime.wasiModule, err = sr.rt.InstantiateModule(option.Context, sr.wasi, anonymous); err != nil {
		return nil, fmt.Errorf("failed to instantiate WASI: %w", err)
	}

	if runtime.envCompiled, err = sr.rt.NewHostModuleBuilder("env").
		NewFunctionBuilder().
		WithFunc(option.ProxyFunction).
		Export("jsFunctionProxy").
		Compile(option.Context); err != nil {
		return nil, fmt.Errorf("failed to setup host module: %w", err)
	}
	if runtime.envModule, err = sr.rt.InstantiateModule(option.Context, runtime.envCompiled, anonymous); err != nil {
		return nil, fmt.Errorf("failed to setup host module: %w", err)
	}
	wasiModule, envModule := runtime.wasiModule, runtime.envModule
	instantiateCtx := experimental.WithImportResolver(option.Context, func(name string) api.Module {
		switch name {
		case wsp1.ModuleName:
			return wasiModule
		case "env":
			return envModule
		}
		return nil
	})

	// fsConfig mounts nothing by default. Option.NoFSMount (the safe
	// default for untrusted sandboxed code -- see pkg/codemode's engine.go)
	// leaves it this way: no wazero preopen at all means every guest
	// file-I/O syscall fails as if the filesystem were empty. Only a
	// caller that explicitly opts out of NoFSMount gets its CWD mounted at
	// "/".
	fsConfig := wazero.NewFSConfig()
	if !runtime.option.NoFSMount {
		fsConfig = fsConfig.WithDirMount(runtime.option.CWD, "/")
	}
	// wazero.NewModuleConfig's environ is empty unless WithEnv is called,
	// which New never does -- so the WASI module (and therefore
	// quickjs-libc's std.getenv/std.getenviron, when std is reachable at
	// all) never observes any of the host process's environment variables.
	// Deliberately not calling WithEnv here is itself the fix for that part
	// of the sandbox-escape surface; see pkg/codemode's
	// TestSandboxHardening_EnvironmentIsNotLeaked for the regression
	// coverage.
	if runtime.module, err = sr.rt.InstantiateModule(
		instantiateCtx,
		sr.qjs,
		wazero.NewModuleConfig().
			WithName("").
			WithStartFunctions(option.StartFunctionName).
			WithSysWalltime().
			WithSysNanotime().
			WithSysNanosleep().
			WithFSConfig(fsConfig).
			WithStdout(option.Stdout).
			WithStderr(option.Stderr),
	); err != nil {
		return nil, fmt.Errorf("failed to instantiate module: %w", err)
	}

	runtime.initializeRuntime()
	succeeded = true

	return runtime, nil
}

func (r *Runtime) Raw() uint64 {
	return r.handle.raw
}

// FreeQJSRuntime frees the QJS runtime.
func (r *Runtime) FreeQJSRuntime() {
	defer func() {
		err := AnyToError(recover())
		if err != nil {
			panic(fmt.Errorf("failed to free QJS runtime: %w", err))
		}
	}()

	r.Call("QJS_Free", r.handle.raw)
}

// Mem returns the WebAssembly memory interface for this runtime.
func (r *Runtime) Mem() *Mem {
	return r.mem
}

// String returns a string representation of the runtime.
func (r *Runtime) String() string {
	return fmt.Sprintf("QJSRuntime: %p", r)
}

// Close cleanly shuts down the runtime and frees all associated resources.
func (r *Runtime) Close() {
	if r == nil {
		return
	}

	// Free QJS runtime handle
	if r.handle != nil {
		r.FreeQJSRuntime()
		r.handle = nil
	}

	r.closeInstances()

	// Clear references
	if r.context != nil {
		r.context = nil
	}

	if r.registry != nil {
		r.registry.Clear()
		r.registry = nil
	}

	// Clear function references
	r.malloc = nil
	r.free = nil
	r.mem = nil
}

// closeInstances closes this run's guest, env and WASI instances, the
// compiled env host module, and the private runtime when there is one.
// Close has no error return of its own (it mirrors io.Closer-style teardown
// used throughout this package), and a close failure here is not
// actionable by any caller during runtime teardown, so errors are
// intentionally discarded.
func (r *Runtime) closeInstances() {
	ctx := context.Background()
	if r.module != nil {
		_ = r.module.Close(ctx)
		r.module = nil
	}
	if r.envModule != nil {
		_ = r.envModule.Close(ctx)
		r.envModule = nil
	}
	if r.envCompiled != nil {
		_ = r.envCompiled.Close(ctx)
		r.envCompiled = nil
	}
	if r.wasiModule != nil {
		_ = r.wasiModule.Close(ctx)
		r.wasiModule = nil
	}
	if r.ownedRuntime != nil {
		_ = r.ownedRuntime.Close(ctx)
		r.ownedRuntime = nil
	}
}

// Load executes a JavaScript file in the runtime's context.
func (r *Runtime) Load(file string, flags ...EvalOptionFunc) (*Value, error) {
	return r.context.Load(file, flags...)
}

// Eval executes JavaScript code in the runtime's context.
func (r *Runtime) Eval(file string, flags ...EvalOptionFunc) (*Value, error) {
	return r.context.Eval(file, flags...)
}

// EvalNoAutoAwait executes JavaScript code in the runtime's context
// without automatically awaiting a promise result -- see
// Context.EvalNoAutoAwait.
func (r *Runtime) EvalNoAutoAwait(file string, flags ...EvalOptionFunc) (*Value, error) {
	return r.context.EvalNoAutoAwait(file, flags...)
}

// Compile compiles JavaScript code to bytecode without executing it.
func (r *Runtime) Compile(file string, flags ...EvalOptionFunc) ([]byte, error) {
	return r.context.Compile(file, flags...)
}

// Context returns the JavaScript execution context for this runtime.
func (r *Runtime) Context() *Context {
	return r.context
}

// Call invokes a WebAssembly function by name with the given arguments.
func (r *Runtime) Call(name string, args ...uint64) *Handle {
	return NewHandle(r, r.call(name, args...))
}

// CallUnPack calls a WebAssembly function and unpacks the returned pointer.
func (r *Runtime) CallUnPack(name string, args ...uint64) (uint32, uint32) {
	return r.mem.UnpackPtr(r.Call(name, args...).raw)
}

// RunPendingJobs drains the runtime's job queue to exhaustion: it runs
// every currently-runnable job (promise reaction callbacks, thenable
// resolution jobs, etc.) until none remain, then returns -- regardless of
// whether any particular promise a caller cares about has settled yet.
// Returns the number of jobs executed (0 if the queue was already empty),
// or an error if a job threw while running (the triggering exception is
// left pending on r.Context(), exactly as any other failing qjs call
// leaves it -- callers check r.Context().HasException()/Exception() the
// same way).
//
// This is the Go binding for QJS_RunPendingJobs, added to qjs.wasm by
// pkg/internal/third_party/qjs/build/job-queue-quiescence.patch (see
// README.vendor.md). Unlike Value.Await (js_std_await), RunPendingJobs
// never blocks waiting for one specific promise to settle and never polls
// for external events, so it cannot hang on a promise that is
// intentionally left pending forever. It is the primitive the code-mode
// host bridge (pkg/codemode) polls to detect that no more synchronous
// progress is possible in the sandbox -- every host function call
// dispatched so far is either settled or genuinely parked awaiting
// approval -- before batching concurrent tool-approval interrupts
// together (e.g. from a guest `Promise.all([...])`).
func (r *Runtime) RunPendingJobs() (int, error) {
	ran := r.Call("QJS_RunPendingJobs", r.handle.raw).Int32()
	if ran < 0 {
		if r.context.HasException() {
			return 0, r.context.Exception()
		}
		return 0, fmt.Errorf("qjs: a pending job failed without recording an exception")
	}
	return int(ran), nil
}

// Malloc allocates memory in the WebAssembly linear memory and return a pointer to it.
func (r *Runtime) Malloc(size uint64) uint64 {
	ptrs, err := r.malloc.Call(r.context, size)
	if err != nil {
		panic(fmt.Errorf("failed to allocate memory: %w", err))
	}

	return ptrs[0]
}

// FreeHandle releases memory allocated in WebAssembly linear memory.
func (r *Runtime) FreeHandle(ptr uint64) {
	if _, err := r.free.Call(r.context, ptr); err != nil {
		panic(fmt.Errorf("failed to free memory: %w", err))
	}
}

// FreeJsValue frees a JavaScript value in the QuickJS runtime.
func (r *Runtime) FreeJsValue(val uint64) {
	r.Call("QJS_FreeValue", r.context.Raw(), val)
}

// NewBytesHandle creates a handle for byte data in WebAssembly memory.
func (r *Runtime) NewBytesHandle(b []byte) *Handle {
	if len(b) == 0 {
		return nil
	}

	ptr := r.Malloc(uint64(len(b)))
	r.mem.MustWrite(uint32(ptr), b)

	return NewHandle(r, ptr)
}

// NewStringHandle creates a handle for string data with null termination.
func (r *Runtime) NewStringHandle(v string) *Handle {
	// Allocate len+1 for null terminator
	ptr := r.Malloc(uint64(len(v) + 1))

	// Write string data
	r.mem.MustWrite(uint32(ptr), []byte(v))
	// Add null terminator
	r.mem.MustWrite(uint32(ptr)+uint32(len(v)), []byte{0})

	return NewHandle(r, ptr)
}

// initializeRuntime sets up the runtime components after module instantiation.
func (r *Runtime) initializeRuntime() {
	r.malloc = r.module.ExportedFunction("malloc")
	r.free = r.module.ExportedFunction("free")
	r.mem = &Mem{mem: r.module.Memory()}
	disableModules := uint64(0)
	if r.option.DisableModules {
		disableModules = 1
	}
	r.handle = r.Call(
		"New_QJS",
		uint64(r.option.MemoryLimit),
		uint64(r.option.MaxStackSize),
		uint64(r.option.MaxExecutionTime),
		uint64(r.option.GCThreshold),
		disableModules,
	)

	r.context.handle = r.Call("QJS_GetContext", r.handle.raw)
	r.context.runtime = r
}

func (r *Runtime) call(name string, args ...uint64) uint64 {
	fn := r.module.ExportedFunction(name)
	if fn == nil {
		panic(fmt.Errorf("WASM function %s not found", name))
	}

	results, err := fn.Call(r.context, args...)
	if err != nil {
		// The Go stack is deliberately not included: this error can reach
		// sandboxed scripts and, through them, a model, and must not leak
		// host source paths.
		panic(fmt.Errorf("failed to call %s: %w", name, err))
	}

	if len(results) == 0 {
		return 0
	}

	return results[0]
}

// Pool manages a collection of reusable QuickJS runtimes.
type Pool struct {
	pools      chan *Runtime
	size       int
	option     Option
	setupFuncs []func(*Runtime) error
	mu         sync.Mutex
}

// NewPool creates a new runtime pool with the specified size and configuration.
func NewPool(size int, option Option, setupFuncs ...func(*Runtime) error) *Pool {
	if size <= 0 {
		panic("pool size must be greater than 0")
	}

	p := &Pool{
		pools:      make(chan *Runtime, size),
		size:       size,
		option:     option,
		setupFuncs: setupFuncs,
	}

	return p
}

// Get returns a runtime from the pool or creates a new one if the pool is empty.
// The caller must call Put() to return the runtime when finished.
func (p *Pool) Get() (*Runtime, error) {
	if p == nil {
		return nil, errors.New("pool is nil")
	}

	// Try to get from pool first
	select {
	case rt := <-p.pools:
		return p.prepareRuntimeForUse(rt), nil
	// Pool is empty, need to create new runtime
	default:
	}

	// Double-check with lock to avoid race conditions
	p.mu.Lock()
	defer p.mu.Unlock()

	select {
	case rt := <-p.pools:
		return p.prepareRuntimeForUse(rt), nil
	default:
		return p.createNewRuntime()
	}
}

// Put returns the runtime back to the pool for reuse.
// If the pool is full, the runtime is closed to prevent resource leaks.
func (p *Pool) Put(rt *Runtime) {
	if rt == nil {
		return
	}

	// Update stack top before returning to pool
	if rt.handle != nil {
		rt.Call("QJS_UpdateStackTop", rt.handle.raw)
	}

	select {
	case p.pools <- rt:
		// Successfully returned to pool
	default:
		// Pool is full, close the runtime
		rt.Close()
	}
}

// prepareRuntimeForUse prepares a pooled runtime for use.
func (p *Pool) prepareRuntimeForUse(rt *Runtime) *Runtime {
	if rt != nil && rt.handle != nil {
		rt.Call("QJS_UpdateStackTop", rt.handle.raw)
	}

	return rt
}

// createNewRuntime creates a new runtime with setup functions applied.
func (p *Pool) createNewRuntime() (*Runtime, error) {
	rt, err := New(p.option)
	if err != nil {
		return nil, fmt.Errorf("failed to create new runtime: %w", err)
	}

	// Apply setup functions
	for i, setupFunc := range p.setupFuncs {
		err := setupFunc(rt)
		if err != nil {
			rt.Close()

			return nil, fmt.Errorf("setup function %d failed: %w", i, err)
		}
	}

	return rt, nil
}

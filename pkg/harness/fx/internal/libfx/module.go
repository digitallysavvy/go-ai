package libfx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// DefaultMemoryLimitPages caps guest linear memory at 1 GiB. fx-core.wasm
// declares no maximum, which would otherwise allow 4 GiB.
const DefaultMemoryLimitPages = 16384

// ErrWasmInterface is matched (errors.Is) by *InterfaceError: the module's
// imports or exports differ from the interface this host implements.
var ErrWasmInterface = errors.New("libfx: fx-core.wasm does not match the pinned host interface")

// InterfaceError lists how a module's interface differs from the pin.
type InterfaceError struct {
	Added   []string // imports the module has and the host lacks
	Removed []string // imports the pin has and the module lacks
	Changed []string // imports whose signature differs
	Missing []string // required exports the module lacks
}

func (e *InterfaceError) Error() string {
	var parts []string
	add := func(label string, items []string) {
		if len(items) > 0 {
			parts = append(parts, label+" "+strings.Join(items, ", "))
		}
	}
	add("unexpected imports:", e.Added)
	add("missing imports:", e.Removed)
	add("changed imports:", e.Changed)
	add("missing exports:", e.Missing)
	return fmt.Sprintf("fx runtime: fx-core.wasm does not match the libfx %s host interface (%s); update the pin with genpin and review the host", Version, strings.Join(parts, "; "))
}

// Unwrap matches ErrWasmInterface.
func (e *InterfaceError) Unwrap() error { return ErrWasmInterface }

func sigString(name string, params, results []api.ValueType) string {
	p := make([]string, len(params))
	for i, t := range params {
		p[i] = api.ValueTypeName(t)
	}
	r := make([]string, len(results))
	for i, t := range results {
		r[i] = api.ValueTypeName(t)
	}
	return fmt.Sprintf("%s(%s)->(%s)", name, strings.Join(p, ","), strings.Join(r, ","))
}

// checkInterface compares a compiled module's imports and exports with the
// pinned table, name for name and signature for signature.
func checkInterface(cm wazero.CompiledModule) error {
	want := map[string]importSig{}
	for _, s := range expectedImports {
		want[s.Module+"."+s.Name] = s
	}
	e := &InterfaceError{}
	seen := map[string]bool{}
	for _, d := range cm.ImportedFunctions() {
		module, name, _ := d.Import()
		key := module + "." + name
		seen[key] = true
		s, ok := want[key]
		if !ok {
			e.Added = append(e.Added, sigString(key, d.ParamTypes(), d.ResultTypes()))
			continue
		}
		if !sameTypes(s.Params, d.ParamTypes()) || !sameTypes(s.Results, d.ResultTypes()) {
			e.Changed = append(e.Changed, sigString(key, s.Params, s.Results)+" is now "+sigString(key, d.ParamTypes(), d.ResultTypes()))
		}
	}
	for key, s := range want {
		if !seen[key] {
			e.Removed = append(e.Removed, sigString(key, s.Params, s.Results))
		}
	}
	for _, m := range cm.ImportedMemories() {
		module, name, _ := m.Import()
		e.Added = append(e.Added, "memory "+module+"."+name)
	}
	funcs := cm.ExportedFunctions()
	mems := cm.ExportedMemories()
	for _, exp := range expectedExports {
		kind, name, _ := strings.Cut(exp, " ")
		switch kind {
		case "func":
			f, ok := funcs[name]
			if !ok || len(f.ParamTypes()) != 0 || len(f.ResultTypes()) != 0 {
				e.Missing = append(e.Missing, exp+"() -> ()")
			}
		case "memory":
			if _, ok := mems[name]; !ok {
				e.Missing = append(e.Missing, exp)
			}
		}
	}
	if len(e.Added)+len(e.Removed)+len(e.Changed)+len(e.Missing) == 0 {
		return nil
	}
	sort.Strings(e.Added)
	sort.Strings(e.Removed)
	sort.Strings(e.Changed)
	return e
}

func sameTypes(a, b []api.ValueType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// host is a wazero runtime with the host modules instantiated and fx-core
// compiled. A CompiledModule may only be instantiated by the Runtime that
// compiled it, so the runtime, host modules and module live together. One
// host is shared by every engine with the same module and memory limit;
// each engine instantiates its own anonymous guest, with its own memory.
type host struct {
	rt          wazero.Runtime
	compiled    wazero.CompiledModule
	interpreter bool
}

type hostKey struct {
	sha256     string
	limitPages uint32
}

var (
	hostsMu sync.Mutex
	hosts   = map[hostKey]*host{}
	// compilationCaches is one cache per directory, shared across hosts.
	compilationCaches = map[string]wazero.CompilationCache{}
)

// compileHook lets tests force the optimizing compiler to fail.
var compileHook func(interpreter bool)

// loadHost returns the shared host for mod, compiling it on first use. A
// failed compile is not cached, so a later call retries.
func loadHost(ctx context.Context, mod *Module, limitPages uint32) (*host, error) {
	if limitPages == 0 {
		limitPages = DefaultMemoryLimitPages
	}
	key := hostKey{mod.SHA256, limitPages}
	hostsMu.Lock()
	defer hostsMu.Unlock()
	if h := hosts[key]; h != nil {
		return h, nil
	}
	cache := compilationCacheFor(mod.CacheDir)
	h, err := newHost(ctx, mod.Bytes, limitPages, cache, false)
	var bug *compilerPanic
	if errors.As(err, &bug) {
		// An optimizing-compiler bug degrades to the interpreter instead of
		// crashing the host (wazero v1.9-v1.11 panic on this module on arm64).
		fmt.Fprintf(os.Stderr, "go-ai fx: wazero compiler failed on fx-core.wasm (%v); using the interpreter\n", bug.value)
		h, err = newHost(ctx, mod.Bytes, limitPages, nil, true)
	}
	if err != nil {
		return nil, err
	}
	hosts[key] = h
	return h, nil
}

type compilerPanic struct{ value any }

func (p *compilerPanic) Error() string { return fmt.Sprintf("wazero compiler panic: %v", p.value) }

func newHost(ctx context.Context, wasm []byte, limitPages uint32, cache wazero.CompilationCache, interpreter bool) (h *host, err error) {
	cfg := wazero.NewRuntimeConfigCompiler()
	if interpreter {
		cfg = wazero.NewRuntimeConfigInterpreter()
	}
	cfg = cfg.WithCloseOnContextDone(true).WithMemoryLimitPages(limitPages)
	if cache != nil {
		cfg = cfg.WithCompilationCache(cache)
	}
	// The runtime outlives the ctx that first loads it.
	rt := wazero.NewRuntimeWithConfig(context.WithoutCancel(ctx), cfg)
	defer func() {
		if r := recover(); r != nil {
			err = &compilerPanic{value: r}
		}
		if err != nil {
			_ = rt.Close(context.WithoutCancel(ctx))
		}
	}()
	if compileHook != nil {
		compileHook(interpreter)
	}
	compiled, err := rt.CompileModule(ctx, wasm)
	if err != nil {
		return nil, fmt.Errorf("fx runtime: compile fx-core.wasm: %w", err)
	}
	if err := checkInterface(compiled); err != nil {
		return nil, err
	}
	if err := instantiateHostModules(ctx, rt); err != nil {
		return nil, err
	}
	return &host{rt: rt, compiled: compiled, interpreter: interpreter}, nil
}

// compilationCacheFor returns the on-disk compilation cache under the
// module's version directory, or an in-memory cache when there is none.
// Callers hold hostsMu.
func compilationCacheFor(versionDir string) wazero.CompilationCache {
	// Without a known wazero version (a replaced module) the cache stays in
	// memory.
	dir := ""
	if v := wazeroVersion(); versionDir != "" && v != "" {
		dir = filepath.Join(versionDir, "wazero-"+v+"-"+goosArch())
	}
	if c := compilationCaches[dir]; c != nil {
		return c
	}
	var c wazero.CompilationCache
	if dir != "" && !compileCacheDirTrusted(versionDir, dir) {
		// Someone else can write here (a squatted temp dir, a shared or
		// loosely permissioned GO_AI_FX_CACHE_DIR): keep machine code in
		// memory rather than load it from disk.
		dir = ""
	}
	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err == nil && privateDir(dir) {
			c, err = wazero.NewCompilationCacheWithDir(dir)
			if err != nil {
				c = nil
			}
		}
	}
	if c == nil {
		c = wazero.NewCompilationCache()
	}
	compilationCaches[dir] = c
	return c
}

func goosArch() string { return runtime.GOOS + "-" + runtime.GOARCH }

// compileCacheDirTrusted reports whether the cache root, the version
// directory and (when it exists) the compile directory are private to this
// user. wazero checks cached machine code only with a CRC32, so a writable
// ancestor would let another user plant code this process runs.
func compileCacheDirTrusted(versionDir, dir string) bool {
	if !privateDir(filepath.Dir(versionDir)) || !privateDir(versionDir) {
		return false
	}
	if _, err := os.Lstat(dir); err == nil {
		return privateDir(dir)
	}
	return true
}

// wazeroModuleVersion keys the on-disk compilation cache, so machine code
// is never shared across wazero releases. wazero's own cache key falls back
// to "dev" when build info lacks its version (test binaries), so the host
// keys it explicitly. TestWazeroModuleVersionMatchesGoMod keeps it in step
// with go.mod.
const wazeroModuleVersion = "v1.12.0"

func wazeroVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == "github.com/tetratelabs/wazero" {
				if dep.Replace != nil {
					// A replaced (possibly local) wazero has no stable version.
					return ""
				}
				return dep.Version
			}
		}
	}
	return wazeroModuleVersion
}

// Prepare resolves fx-core.wasm (downloading and caching it on first use),
// verifies it, compiles it and checks its import table, without running it.
// Hosts call it at boot so the first session does not pay for the download
// or the compile.
func Prepare(ctx context.Context, src Source, limitPages uint32) (*Module, error) {
	mod, err := Resolve(ctx, src)
	if err != nil {
		return nil, err
	}
	if _, err := loadHost(ctx, mod, limitPages); err != nil {
		return nil, err
	}
	return mod, nil
}

package libfx

import (
	"bytes"
	"context"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// A minimal wasm binary encoder for fake guests, so the ABI tests need no
// toolchain and no network.

func uleb(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			out = append(out, b|0x80)
			continue
		}
		return append(out, b)
	}
}

func sleb(v int64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if (v == 0 && b&0x40 == 0) || (v == -1 && b&0x40 != 0) {
			return append(out, b)
		}
		out = append(out, b|0x80)
	}
}

func wasmName(s string) []byte { return append(uleb(uint64(len(s))), s...) }

func section(id byte, body []byte) []byte {
	return append(append([]byte{id}, uleb(uint64(len(body)))...), body...)
}

func vec(items ...[]byte) []byte {
	out := uleb(uint64(len(items)))
	for _, it := range items {
		out = append(out, it...)
	}
	return out
}

func funcType(params, results []api.ValueType) []byte {
	out := []byte{0x60}
	out = append(out, uleb(uint64(len(params)))...)
	out = append(out, params...)
	out = append(out, uleb(uint64(len(results)))...)
	return append(out, results...)
}

// guestSpec describes a fake guest: its imports, the body of _start (raw
// instructions, without the final end), and data placed in memory.
type guestSpec struct {
	imports    []importSig
	start      []byte
	data       map[uint32][]byte
	noStart    bool
	noMemory   bool
	startParam bool // _start takes an i32 (a wrong export signature)
}

func (g guestSpec) encode() []byte {
	var types [][]byte
	for _, imp := range g.imports {
		types = append(types, funcType(imp.Params, imp.Results))
	}
	startType := funcType(nil, nil)
	if g.startParam {
		startType = funcType([]api.ValueType{api.ValueTypeI32}, nil)
	}
	types = append(types, startType)
	var imports [][]byte
	for i, imp := range g.imports {
		entry := append(wasmName(imp.Module), wasmName(imp.Name)...)
		entry = append(entry, 0x00)
		entry = append(entry, uleb(uint64(i))...)
		imports = append(imports, entry)
	}
	out := []byte("\x00asm\x01\x00\x00\x00")
	out = append(out, section(1, vec(types...))...)
	out = append(out, section(2, vec(imports...))...)
	if !g.noStart {
		out = append(out, section(3, vec(uleb(uint64(len(g.imports)))))...)
	}
	if !g.noMemory {
		out = append(out, section(5, vec([]byte{0x00, 0x01}))...)
	}
	var exports [][]byte
	if !g.noStart {
		exports = append(exports, append(append(wasmName("_start"), 0x00), uleb(uint64(len(g.imports)))...))
	}
	if !g.noMemory {
		exports = append(exports, append(append(wasmName("memory"), 0x02), 0x00))
	}
	out = append(out, section(7, vec(exports...))...)
	if !g.noStart {
		body := append([]byte{0x00}, g.start...)
		body = append(body, 0x0b)
		out = append(out, section(10, vec(append(uleb(uint64(len(body))), body...)))...)
	}
	if len(g.data) > 0 {
		var segs [][]byte
		for off, d := range g.data {
			seg := []byte{0x00, 0x41}
			seg = append(seg, sleb(int64(off))...)
			seg = append(seg, 0x0b)
			seg = append(seg, uleb(uint64(len(d)))...)
			seg = append(seg, d...)
			segs = append(segs, seg)
		}
		out = append(out, section(11, vec(segs...))...)
	}
	return out
}

// importIndex returns the function index of a pinned import.
func importIndex(t *testing.T, module, name string) uint64 {
	t.Helper()
	for i, imp := range expectedImports {
		if imp.Module == module && imp.Name == name {
			return uint64(i)
		}
	}
	t.Fatalf("no pinned import %s.%s", module, name)
	return 0
}

// Instruction helpers.
func i32Const(v int32) []byte { return append([]byte{0x41}, sleb(int64(v))...) }
func call(idx uint64) []byte  { return append([]byte{0x10}, uleb(idx)...) }

var drop = []byte{0x1a}

func code(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

// pinnedGuest is a fake guest with the full pinned import table.
func pinnedGuest(start []byte, data map[uint32][]byte) []byte {
	return guestSpec{imports: expectedImports, start: start, data: data}.encode()
}

// memoryGuest instantiates a module that only exports one page of memory,
// for calling host functions directly.
func memoryGuest(t *testing.T) api.Module {
	t.Helper()
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter())
	t.Cleanup(func() { _ = rt.Close(ctx) })
	mod, err := rt.InstantiateWithConfig(ctx, guestSpec{noStart: true}.encode(), wazero.NewModuleConfig().WithStartFunctions())
	if err != nil {
		t.Fatal(err)
	}
	return mod
}

// testInstance returns an instance and a ctx that carries it.
func testInstance(t *testing.T) (*instance, context.Context) {
	t.Helper()
	runCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	inst := &instance{
		args:         []string{"fx", "acp"},
		stdin:        newByteQueue(),
		runCtx:       runCtx,
		effects:      map[int]context.CancelFunc{},
		streams:      map[int32]*stream{},
		inbound:      map[uint32][]byte{},
		outbound:     map[int32][]byte{},
		nextOutbound: 1,
	}
	inst.out = &coreOutput{handle: func([]byte, int) error { return nil }}
	return inst, context.WithValue(runCtx, instanceKey{}, inst)
}

// callHost runs a host function with the given i32 args and returns its
// first result (0 when it has none).
func callHost(ctx context.Context, mod api.Module, fn hostFunc, args ...uint64) int32 {
	stack := make([]uint64, max(len(args), 1))
	copy(stack, args)
	fn(ctx, mod, stack)
	return api.DecodeI32(stack[0])
}

func readMem(t *testing.T, mod api.Module, ptr, n uint32) []byte {
	t.Helper()
	b, ok := mod.Memory().Read(ptr, n)
	if !ok {
		t.Fatalf("read %d@%d out of range", n, ptr)
	}
	return append([]byte(nil), b...)
}

func writeMem(t *testing.T, mod api.Module, ptr uint32, data []byte) {
	t.Helper()
	if !mod.Memory().Write(ptr, data) {
		t.Fatalf("write %d@%d out of range", len(data), ptr)
	}
}

func u32At(t *testing.T, mod api.Module, ptr uint32) uint32 {
	t.Helper()
	v, ok := mod.Memory().ReadUint32Le(ptr)
	if !ok {
		t.Fatal("out of range")
	}
	return v
}

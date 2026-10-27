package libfx

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tetratelabs/wazero/api"
)

// fakeSource runs a fake guest: unpinned bytes, never cached.
func fakeSource(t *testing.T, wasm []byte) Source {
	return Source{Bytes: wasm, AllowUnpinned: true, CacheDir: t.TempDir()}
}

func fdCall(t *testing.T, name string, fd, iov, nout int32) []byte {
	return code(i32Const(fd), i32Const(iov), i32Const(1), i32Const(nout), call(importIndex(t, wasiModule, name)), drop)
}

// lineData places one stdout line at 2048 with its iovec at 1032, and a
// 4 KiB read buffer at 4096 with its iovec at 1024.
func lineData(line string) map[uint32][]byte {
	return map[uint32][]byte{
		1024: append(append(le32b(4096), le32b(4096)...), append(le32b(2048), le32b(uint32(len(line)))...)...),
		2048: []byte(line),
	}
}

func startFake(t *testing.T, wasm []byte, opts Options) *Engine {
	t.Helper()
	opts.Wasm = fakeSource(t, wasm)
	e, err := Start(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func waitDone(t *testing.T, e *Engine) {
	t.Helper()
	select {
	case <-e.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("core did not exit")
	}
}

func TestEngineProcExitCode(t *testing.T) {
	wasm := pinnedGuest(code(i32Const(3), call(importIndex(t, wasiModule, "proc_exit"))), nil)
	e := startFake(t, wasm, Options{})
	waitDone(t, e)
	var exit *ExitError
	if !errors.As(e.Err(), &exit) || exit.Code != 3 || e.ExitCode() != 3 {
		t.Fatalf("exit = %v", e.Err())
	}
	if _, err := e.Request(context.Background(), "initialize", nil); !errors.As(err, &exit) {
		t.Fatalf("request after exit: %v", err)
	}
	if err := e.Close(context.Background()); !errors.As(err, &exit) {
		t.Fatalf("close after abnormal exit: %v", err)
	}
}

func TestEngineNotificationsAndCleanExit(t *testing.T) {
	line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","n":1}}` + "\n"
	wasm := pinnedGuest(fdCall(t, "fd_write", 1, 1032, 1104), lineData(line))
	var mu sync.Mutex
	var got []string
	e := startFake(t, wasm, Options{OnNotification: func(method string, params json.RawMessage) {
		mu.Lock()
		got = append(got, method+" "+string(params))
		mu.Unlock()
	}})
	waitDone(t, e)
	if err := e.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0] != `session/update {"sessionId":"s","n":1}` {
		t.Fatalf("notifications %q", got)
	}
	if e.Err() != nil || e.ExitCode() != 0 {
		t.Fatalf("clean exit: %v %d", e.Err(), e.ExitCode())
	}
}

func TestEngineRequestRoundTripThroughStdio(t *testing.T) {
	// The guest reads one frame, answers request id 1, then reads until EOF.
	reply := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}` + "\n"
	wasm := pinnedGuest(code(
		fdCall(t, "fd_read", 0, 1024, 1100),
		fdCall(t, "fd_write", 1, 1032, 1104),
		fdCall(t, "fd_read", 0, 1024, 1100),
	), lineData(reply))
	e := startFake(t, wasm, Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := e.Request(ctx, "initialize", map[string]any{"protocolVersion": 1})
	if err != nil || string(res) != `{"protocolVersion":1}` {
		t.Fatalf("request = %s, %v", res, err)
	}
	// Close closes stdin; the guest's blocked fd_read sees EOF and returns.
	start := time.Now()
	if err := e.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > closeGrace/2 {
		t.Fatal("close waited for the grace period instead of EOF")
	}
	if _, err := e.Request(ctx, "x", nil); err == nil {
		t.Fatal("request after close succeeded")
	}
}

func TestEngineRPCErrorAndRequestCancel(t *testing.T) {
	reply := `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"nope","data":{"code":"X"}}}` + "\n"
	wasm := pinnedGuest(code(
		fdCall(t, "fd_read", 0, 1024, 1100),
		fdCall(t, "fd_write", 1, 1032, 1104),
		fdCall(t, "fd_read", 0, 1024, 1100),
		fdCall(t, "fd_read", 0, 1024, 1100),
	), lineData(reply))
	e := startFake(t, wasm, Options{})
	defer e.Close(context.Background()) //nolint:errcheck
	_, err := e.Request(context.Background(), "libfx/new", nil)
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Message != "nope" || rpc.Code != -32000 || string(rpc.Data) != `{"code":"X"}` {
		t.Fatalf("rpc error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := e.Request(ctx, "never", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled request: %v", err)
	}
}

func TestEngineCloseStopsARunawayGuest(t *testing.T) {
	loop := []byte{0x03, 0x40, 0x0c, 0x00, 0x0b} // loop; br 0; end
	e := startFake(t, pinnedGuest(loop, nil), Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := e.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("close took %v", time.Since(start))
	}
	select {
	case <-e.Done():
	default:
		t.Fatal("not done after close")
	}
}

func TestEngineAbortsOnInvalidCoreOutput(t *testing.T) {
	wasm := pinnedGuest(fdCall(t, "fd_write", 1, 1032, 1104), lineData("not json\n"))
	e := startFake(t, wasm, Options{})
	waitDone(t, e)
	if err := e.Err(); err == nil || !strings.Contains(err.Error(), "not JSON") {
		t.Fatalf("err = %v", err)
	}
	_ = e.Close(context.Background())
}

// echoRead reads stdin into 4096 and copies what it read to stderr, using
// an iovec at 1040 whose length is set from nread at run time.
func echoRead(t *testing.T) []byte {
	return code(
		fdCall(t, "fd_read", 0, 1024, 1100),
		i32Const(1044), i32Const(1100), []byte{0x28, 0x02, 0x00}, []byte{0x36, 0x02, 0x00}, // mem[1044] = mem[1100]
		fdCall(t, "fd_write", 2, 1040, 1108),
	)
}

type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func TestEngineAnswersCoreRequests(t *testing.T) {
	// The guest asks for a permission, a journal flush and a host tool, then
	// echoes the host's answers to stderr.
	lines := `{"jsonrpc":"2.0","id":"p1","method":"session/request_permission","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":5,"method":"libfx/journal_flush","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":7,"method":"libfx/tool_call","params":{"name":"t","input":{"a":1},"toolCallId":"c"}}` + "\n" +
		`{"jsonrpc":"2.0","id":8,"method":"libfx/unknown","params":{}}` + "\n"
	data := lineData(lines)
	data[1040] = append(le32b(4096), le32b(0)...)
	wasm := pinnedGuest(code(fdCall(t, "fd_write", 1, 1032, 1104), echoRead(t), echoRead(t), echoRead(t), echoRead(t)), data)
	var stderr lockedBuffer
	var tool string
	var toolMu sync.Mutex
	e := startFake(t, wasm, Options{Stderr: &stderr, ExecuteTool: func(_ context.Context, name string, input json.RawMessage, callID string) (ToolResult, error) {
		toolMu.Lock()
		tool = name + " " + string(input) + " " + callID
		toolMu.Unlock()
		return ToolResult{IsError: true}, nil
	}})
	wants := []string{
		`{"id":"p1","jsonrpc":"2.0","result":{"outcome":{"outcome":"cancelled"}}}`,
		`{"id":5,"jsonrpc":"2.0","result":{}}`,
		`{"id":7,"jsonrpc":"2.0","result":{"content":"Tool t failed without a message","isError":true}}`,
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		out := stderr.String()
		all := true
		for _, w := range wants {
			all = all && strings.Contains(out, w)
		}
		if all {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stderr %q is missing answers %q", out, wants)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := e.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	toolMu.Lock()
	defer toolMu.Unlock()
	if tool != `t {"a":1} c` {
		t.Fatalf("tool call %q", tool)
	}
	if strings.Contains(stderr.String(), `"id":8`) {
		t.Fatal("an unknown core request was answered; libfx drops it")
	}
}

func TestStartRejectsOversizedOptions(t *testing.T) {
	_, err := Start(context.Background(), Options{Model: strings.Repeat("m", maxModelBytes+1)})
	if err == nil || !strings.Contains(err.Error(), "model exceeds") {
		t.Fatalf("err = %v", err)
	}
}

func TestImportGuard(t *testing.T) {
	without := func(name string) []importSig {
		var out []importSig
		for _, s := range expectedImports {
			if s.Name != name {
				out = append(out, s)
			}
		}
		return out
	}
	changed := append([]importSig(nil), expectedImports...)
	for i, s := range changed {
		if s.Name == "fd_write" {
			changed[i].Params = []api.ValueType{api.ValueTypeI32}
		}
	}
	cases := []struct {
		name string
		spec guestSpec
		want string
	}{
		{"added", guestSpec{imports: append(append([]importSig(nil), expectedImports...), importSig{"fx", "fx_term_size", []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, nil})}, "unexpected imports: fx.fx_term_size(i32,i32)->()"},
		{"removed", guestSpec{imports: without("proc_exit")}, "missing imports: wasi_snapshot_preview1.proc_exit(i32)->()"},
		{"changed", guestSpec{imports: changed}, "changed imports: wasi_snapshot_preview1.fd_write(i32,i32,i32,i32)->(i32) is now wasi_snapshot_preview1.fd_write(i32)->(i32)"},
		{"no start", guestSpec{imports: expectedImports, noStart: true}, "missing exports: func _start() -> ()"},
		{"start signature", guestSpec{imports: expectedImports, startParam: true}, "missing exports: func _start() -> ()"},
		{"no memory", guestSpec{imports: expectedImports, noMemory: true}, "missing exports: memory memory"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Start(context.Background(), Options{Wasm: fakeSource(t, c.spec.encode())})
			var ie *InterfaceError
			if !errors.Is(err, ErrWasmInterface) || !errors.As(err, &ie) {
				t.Fatalf("err = %v", err)
			}
			if !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "libfx "+Version) {
				t.Fatalf("error %q does not contain %q", err, c.want)
			}
		})
	}
}

func TestCompilerPanicFallsBackToInterpreter(t *testing.T) {
	defer func() { compileHook = nil }()
	compileHook = func(interpreter bool) {
		if !interpreter {
			panic("BUG")
		}
	}
	// A distinct guest, so no earlier test cached its host.
	wasm := pinnedGuest(code(i32Const(9), call(importIndex(t, wasiModule, "proc_exit"))), nil)
	mod, err := Resolve(context.Background(), fakeSource(t, wasm))
	if err != nil {
		t.Fatal(err)
	}
	h, err := loadHost(context.Background(), mod, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !h.interpreter {
		t.Fatal("host is not on the interpreter")
	}
	e := startFake(t, wasm, Options{})
	waitDone(t, e)
	if e.ExitCode() != 9 {
		t.Fatalf("exit code %d", e.ExitCode())
	}
	_ = e.Close(context.Background())
}

func TestWazeroModuleVersionMatchesGoMod(t *testing.T) {
	data, err := readGoMod()
	if err != nil {
		t.Skip(err)
	}
	if !strings.Contains(data, "github.com/tetratelabs/wazero "+wazeroModuleVersion+"\n") {
		t.Fatalf("go.mod's wazero version differs from wazeroModuleVersion %s; update module.go", wazeroModuleVersion)
	}
}

func readGoMod() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			return string(data), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}

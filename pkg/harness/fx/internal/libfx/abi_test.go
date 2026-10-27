package libfx

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tetratelabs/wazero/api"
)

func le64(v uint64) []byte  { return binary.LittleEndian.AppendUint64(nil, v) }
func le32b(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

func TestHostImplementsExactlyThePinnedImports(t *testing.T) {
	pinned := map[string]bool{}
	for _, s := range expectedImports {
		if _, ok := hostFuncFor(s.Module, s.Name); !ok {
			t.Errorf("no host implementation for %s.%s", s.Module, s.Name)
		}
		pinned[s.Module+"."+s.Name] = true
	}
	for name := range wasiFuncs {
		if !pinned[wasiModule+"."+name] {
			t.Errorf("host defines %s.%s, which fx-core %s does not import", wasiModule, name, Version)
		}
	}
	for name := range fxFuncs {
		if !pinned[fxModule+"."+name] {
			t.Errorf("host defines %s.%s, which fx-core %s does not import", fxModule, name, Version)
		}
	}
	if len(expectedImports) != 55 {
		t.Errorf("pinned imports = %d, want 55 for libfx 0.0.14", len(expectedImports))
	}
}

func TestArgsAndEnviron(t *testing.T) {
	mod := memoryGuest(t)
	inst, ctx := testInstance(t)
	inst.env = []string{"AI_GATEWAY_API_KEY=k", "FX_MODEL=m"}
	if r := callHost(ctx, mod, wasiFuncs["args_sizes_get"], 0, 4); r != 0 {
		t.Fatal(r)
	}
	if u32At(t, mod, 0) != 2 || u32At(t, mod, 4) != uint32(len("fx\x00acp\x00")) {
		t.Fatalf("args sizes %d %d", u32At(t, mod, 0), u32At(t, mod, 4))
	}
	if r := callHost(ctx, mod, wasiFuncs["args_get"], 100, 200); r != 0 {
		t.Fatal(r)
	}
	if got := readMem(t, mod, 200, 7); string(got) != "fx\x00acp\x00" || u32At(t, mod, 104) != 203 {
		t.Fatalf("argv %q ptr1 %d", got, u32At(t, mod, 104))
	}
	callHost(ctx, mod, wasiFuncs["environ_sizes_get"], 0, 4)
	if u32At(t, mod, 0) != 2 || u32At(t, mod, 4) != uint32(len(inst.env[0])+len(inst.env[1])+2) {
		t.Fatal("environ sizes")
	}
	callHost(ctx, mod, wasiFuncs["environ_get"], 100, 200)
	if got := readMem(t, mod, 200, uint32(len(inst.env[0]))); string(got) != inst.env[0] {
		t.Fatalf("environ %q", got)
	}
	// Out-of-range pointers fault instead of trapping the host.
	if r := callHost(ctx, mod, wasiFuncs["args_get"], 1<<20, 0); r != errnoFault {
		t.Fatalf("args_get out of range = %d", r)
	}
}

func TestWasiConstantsAndStat(t *testing.T) {
	mod := memoryGuest(t)
	_, ctx := testInstance(t)
	for name, want := range map[string]int32{
		"fd_close": 0, "fd_sync": 0, "fd_seek": errnoSpipe, "fd_prestat_get": errnoBadf,
		"path_open": errnoNosys, "fd_readdir": errnoNosys, "path_unlink_file": errnoNosys,
	} {
		if r := callHost(ctx, mod, wasiFuncs[name], 0, 0, 0, 0, 0, 0, 0, 0, 0); r != want {
			t.Errorf("%s = %d, want %d", name, r, want)
		}
	}
	callHost(ctx, mod, wasiFuncs["fd_fdstat_get"], 1, 0)
	stat := readMem(t, mod, 0, 24)
	if stat[0] != 2 || !bytes.Equal(stat[8:], bytes.Repeat([]byte{0xff}, 16)) {
		t.Fatalf("fdstat fd1 %x", stat)
	}
	callHost(ctx, mod, wasiFuncs["fd_fdstat_get"], 5, 0)
	if readMem(t, mod, 0, 1)[0] != 0 {
		t.Fatal("fdstat fd5 filetype")
	}
	callHost(ctx, mod, wasiFuncs["clock_res_get"], 0, 0)
	if v, _ := mod.Memory().ReadUint64Le(0); v != 1_000_000 {
		t.Fatalf("clock_res %d", v)
	}
	callHost(ctx, mod, clockTimeGet, 0, 0, 0)
	wall, _ := mod.Memory().ReadUint64Le(0)
	if d := time.Duration(time.Now().UnixNano() - int64(wall)); d < 0 || d > time.Minute {
		t.Fatalf("realtime off by %v", d)
	}
	callHost(ctx, mod, clockTimeGet, 1, 0, 0)
	m1, _ := mod.Memory().ReadUint64Le(0)
	callHost(ctx, mod, clockTimeGet, 1, 0, 0)
	m2, _ := mod.Memory().ReadUint64Le(0)
	if m2 < m1 {
		t.Fatal("monotonic clock went backwards")
	}
	writeMem(t, mod, 0, make([]byte, 32))
	callHost(ctx, mod, wasiFuncs["random_get"], 0, 32)
	if bytes.Equal(readMem(t, mod, 0, 32), make([]byte, 32)) {
		t.Fatal("random_get wrote nothing")
	}
}

// iovec writes one iovec at ptr describing (data, len).
func iovec(t *testing.T, mod api.Module, ptr, data, length uint32) {
	writeMem(t, mod, ptr, append(le32b(data), le32b(length)...))
}

func TestFdWriteRoutesStdoutLinesAndStderr(t *testing.T) {
	mod := memoryGuest(t)
	inst, ctx := testInstance(t)
	var lines []string
	inst.out = &coreOutput{handle: func(line []byte, size int) error {
		lines = append(lines, string(line))
		return nil
	}}
	var stderr bytes.Buffer
	inst.stderr = &stderr
	writeMem(t, mod, 1000, []byte("{\"a\":1}\n{\"b\""))
	writeMem(t, mod, 2000, []byte(":2}\n\n"))
	iovec(t, mod, 0, 1000, 12)
	iovec(t, mod, 8, 2000, 5)
	if r := callHost(ctx, mod, fdWrite, 1, 0, 2, 100); r != 0 {
		t.Fatal(r)
	}
	if u32At(t, mod, 100) != 17 {
		t.Fatalf("nwritten %d", u32At(t, mod, 100))
	}
	if strings.Join(lines, "|") != `{"a":1}|{"b":2}` {
		t.Fatalf("lines %q", lines)
	}
	writeMem(t, mod, 1000, []byte("diag"))
	iovec(t, mod, 0, 1000, 4)
	callHost(ctx, mod, fdWrite, 2, 0, 1, 100)
	if stderr.String() != "diag" {
		t.Fatalf("stderr %q", stderr.String())
	}
	// Other fds are accepted and dropped.
	if r := callHost(ctx, mod, fdWrite, 7, 0, 1, 100); r != 0 || u32At(t, mod, 100) != 4 {
		t.Fatal("fd 7")
	}
}

func TestFdWriteRejectsOversizedMessage(t *testing.T) {
	mod := memoryGuest(t)
	_, ctx := testInstance(t)
	iovec(t, mod, 0, 0, MaxCoreMessageBytes+1)
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(error).Error(), "64 MiB") {
			t.Fatalf("recover = %v", r)
		}
	}()
	callHost(ctx, mod, fdWrite, 1, 0, 1, 100)
}

func TestCoreOutputLimitsAndErrors(t *testing.T) {
	o := &coreOutput{handle: func([]byte, int) error { return nil }}
	if err := o.write([]byte{0xff, '\n'}); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	o = &coreOutput{handle: func([]byte, int) error { return nil }}
	if err := o.write([]byte(`{"partial"`)); err != nil {
		t.Fatal(err)
	}
	if err := o.finish(); err == nil {
		t.Fatal("finish within a message succeeded")
	}
	o = &coreOutput{handle: func([]byte, int) error { return nil }, buf: make([]byte, MaxCoreMessageBytes)}
	if err := o.write([]byte("x\n")); err == nil || !strings.Contains(err.Error(), "64 MiB") {
		t.Fatalf("oversized line: %v", err)
	}
	var sizes []int
	o = &coreOutput{handle: func(_ []byte, size int) error { sizes = append(sizes, size); return nil }}
	_ = o.write([]byte("ab"))
	_ = o.write([]byte("c\n"))
	if len(sizes) != 1 || sizes[0] != 4 {
		t.Fatalf("sizes %v", sizes)
	}
}

func TestFdWriteBlocksForBackpressure(t *testing.T) {
	mod := memoryGuest(t)
	inst, ctx := testInstance(t)
	release := make(chan struct{})
	inst.out = &coreOutput{handle: func([]byte, int) error { <-release; return nil }}
	writeMem(t, mod, 1000, []byte("{}\n"))
	iovec(t, mod, 0, 1000, 3)
	done := make(chan int32)
	go func() { done <- callHost(ctx, mod, fdWrite, 1, 0, 1, 100) }()
	select {
	case <-done:
		t.Fatal("fd_write returned while the handler was blocked")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if r := <-done; r != 0 {
		t.Fatal(r)
	}
}

func TestFdReadBlocksUntilDataThenEOF(t *testing.T) {
	mod := memoryGuest(t)
	inst, ctx := testInstance(t)
	if r := callHost(ctx, mod, fdRead, 3, 0, 1, 100); r != errnoBadf {
		t.Fatalf("fd 3 = %d", r)
	}
	iovec(t, mod, 0, 1000, 4)
	iovec(t, mod, 8, 2000, 4)
	done := make(chan int32)
	go func() { done <- callHost(ctx, mod, fdRead, 0, 0, 2, 100) }()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("fd_read returned with no data")
	default:
	}
	if err := inst.stdin.push([]byte("abcdef")); err != nil {
		t.Fatal(err)
	}
	if r := <-done; r != 0 {
		t.Fatal(r)
	}
	if u32At(t, mod, 100) != 6 || string(readMem(t, mod, 1000, 4)) != "abcd" || string(readMem(t, mod, 2000, 2)) != "ef" {
		t.Fatalf("nread %d", u32At(t, mod, 100))
	}
	inst.stdin.close()
	if r := callHost(ctx, mod, fdRead, 0, 0, 1, 100); r != 0 || u32At(t, mod, 100) != 0 {
		t.Fatal("EOF after close")
	}
	if err := inst.stdin.push([]byte("x")); !errors.Is(err, errStdinClosed) {
		t.Fatalf("push after close: %v", err)
	}
}

// subscription writes a 48-byte poll_oneoff subscription.
func subscription(userdata uint64, tag byte, timeoutNS uint64) []byte {
	s := make([]byte, 48)
	copy(s, le64(userdata))
	s[8] = tag
	copy(s[24:], le64(timeoutNS))
	return s
}

func TestPollOneoff(t *testing.T) {
	mod := memoryGuest(t)
	inst, ctx := testInstance(t)
	subs := append(subscription(7, 0, uint64(30*time.Millisecond)), subscription(9, 1, 0)...)
	writeMem(t, mod, 0, subs)

	// Timeout: one event with the first subscription's userdata.
	start := time.Now()
	if r := callHost(ctx, mod, pollOneoff, 0, 500, 2, 600); r != 0 {
		t.Fatal(r)
	}
	if el := time.Since(start); el < 25*time.Millisecond {
		t.Fatalf("returned after %v, before the 30 ms timeout", el)
	}
	ev := readMem(t, mod, 500, 32)
	if binary.LittleEndian.Uint64(ev) != 7 || ev[10] != 0 || u32At(t, mod, 600) != 1 {
		t.Fatalf("timeout event %x", ev)
	}

	// Data waiting: the fd_read subscription fires at once.
	_ = inst.stdin.push([]byte("x"))
	if r := callHost(ctx, mod, pollOneoff, 0, 500, 2, 600); r != 0 {
		t.Fatal(r)
	}
	ev = readMem(t, mod, 500, 32)
	if binary.LittleEndian.Uint64(ev) != 9 || ev[10] != 1 {
		t.Fatalf("fd_read event %x", ev)
	}
	inst.stdin.read(1)

	// A push wakes a long wait early.
	writeMem(t, mod, 0, subscription(7, 0, uint64(time.Hour)))
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = inst.stdin.push([]byte("y"))
	}()
	start = time.Now()
	callHost(ctx, mod, pollOneoff, 0, 500, 1, 600)
	if time.Since(start) > 5*time.Second {
		t.Fatal("push did not wake poll_oneoff")
	}
	if r := callHost(ctx, mod, pollOneoff, 0, 500, 0, 600); r != errnoFault {
		t.Fatalf("zero subscriptions = %d", r)
	}
}

// putRequestArgs writes method/url/headers/body at fixed offsets and
// returns the eight (ptr,len) stack values.
func putRequestArgs(t *testing.T, mod api.Module, method, url, headers, body string) []uint64 {
	var out []uint64
	off := uint32(4096)
	for _, s := range []string{method, url, headers, body} {
		writeMem(t, mod, off, []byte(s))
		out = append(out, uint64(off), uint64(len(s)))
		off += 4096
	}
	return out
}

type slowBody struct {
	chunks chan []byte
	closed chan struct{}
	once   sync.Once
}

func newSlowBody() *slowBody {
	return &slowBody{chunks: make(chan []byte), closed: make(chan struct{})}
}

func (b *slowBody) Read(p []byte) (int, error) {
	select {
	case c, ok := <-b.chunks:
		if !ok {
			return 0, io.EOF
		}
		return copy(p, c), nil
	case <-b.closed:
		return 0, errors.New("closed")
	}
}

func (b *slowBody) Close() error { b.once.Do(func() { close(b.closed) }); return nil }

func TestHTTPStreamLifecycle(t *testing.T) {
	mod := memoryGuest(t)
	inst, ctx := testInstance(t)
	respond := make(chan struct{})
	body := newSlowBody()
	var gotReq *http.Request
	var gotBody string
	inst.fetch = func(req *http.Request) (*http.Response, error) {
		gotReq = req
		b, _ := io.ReadAll(req.Body)
		gotBody = string(b)
		select {
		case <-respond:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		return &http.Response{StatusCode: 201, Body: body, Header: http.Header{}}, nil
	}
	args := putRequestArgs(t, mod, "POST", "https://ai-gateway.vercel.sh/v1/ai/language-model", `[{"name":"x-a","value":"1"},{"name":"x-a","value":"2"}]`, `{"q":1}`)
	h := callHost(ctx, mod, streamOpen, args...)
	if h < 1 {
		t.Fatalf("handle %d", h)
	}
	start := time.Now()
	if r := callHost(ctx, mod, streamStatus, uint64(h), 0); r != 0 {
		t.Fatalf("status before response = %d", r)
	}
	if el := time.Since(start); el < 40*time.Millisecond || el > 2*time.Second {
		t.Fatalf("not-ready status took %v, want about 50 ms", el)
	}
	close(respond)
	var r int32
	for r = 0; r == 0; {
		r = callHost(ctx, mod, streamStatus, uint64(h), 0)
	}
	if r != 1 {
		t.Fatalf("status = %d", r)
	}
	if v, _ := mod.Memory().ReadUint16Le(0); v != 201 {
		t.Fatalf("status code %d", v)
	}
	if gotReq.Method != "POST" || gotBody != `{"q":1}` || strings.Join(gotReq.Header.Values("x-a"), ",") != "1,2" {
		t.Fatalf("request %s %q %v", gotReq.Method, gotBody, gotReq.Header)
	}
	// No chunk within 50 ms: -3.
	if r := callHost(ctx, mod, streamNext, uint64(h), 100, 4); r != -3 {
		t.Fatalf("next with no chunk = %d", r)
	}
	go func() { body.chunks <- []byte("hello world") }()
	for r = -3; r == -3; {
		r = callHost(ctx, mod, streamNext, uint64(h), 100, 4)
	}
	if r != 4 || string(readMem(t, mod, 100, 4)) != "hell" {
		t.Fatalf("next = %d %q", r, readMem(t, mod, 100, 4))
	}
	// Leftovers come before new reads.
	if r := callHost(ctx, mod, streamNext, uint64(h), 100, 64); r != 7 || string(readMem(t, mod, 100, 7)) != "o world" {
		t.Fatalf("leftover = %d", r)
	}
	close(body.chunks)
	for r = -3; r == -3; {
		r = callHost(ctx, mod, streamNext, uint64(h), 100, 64)
	}
	if r != 0 {
		t.Fatalf("EOF = %d", r)
	}
	if r := callHost(ctx, mod, streamNext, uint64(h), 100, 64); r != 0 {
		t.Fatalf("after EOF = %d", r)
	}
	callHost(ctx, mod, streamClose, uint64(h))
	if r := callHost(ctx, mod, streamStatus, uint64(h), 0); r != -1 {
		t.Fatalf("closed handle status = %d", r)
	}
	if r := callHost(ctx, mod, streamNext, 999, 0, 1); r != -1 {
		t.Fatalf("unknown handle next = %d", r)
	}
}

func TestHTTPStreamAbortAndError(t *testing.T) {
	mod := memoryGuest(t)
	inst, ctx := testInstance(t)
	body := newSlowBody()
	inst.fetch = func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/fail" {
			return nil, errors.New("boom")
		}
		return &http.Response{StatusCode: 200, Body: body, Header: http.Header{}}, nil
	}
	fail := callHost(ctx, mod, streamOpen, putRequestArgs(t, mod, "GET", "https://ai-gateway.vercel.sh/fail", "", "")...)
	var r int32
	for r = 0; r == 0; {
		r = callHost(ctx, mod, streamStatus, uint64(fail), 0)
	}
	if r != -1 {
		t.Fatalf("failed fetch status = %d", r)
	}
	bad := callHost(ctx, mod, streamOpen, putRequestArgs(t, mod, "GET", "https://ai-gateway.vercel.sh/", "not json", "")...)
	for r = 0; r == 0; {
		r = callHost(ctx, mod, streamStatus, uint64(bad), 0)
	}
	if r != -1 {
		t.Fatalf("bad headers status = %d", r)
	}
	h := callHost(ctx, mod, streamOpen, putRequestArgs(t, mod, "GET", "https://ai-gateway.vercel.sh/ok", "[]", "")...)
	for r = 0; r == 0; {
		r = callHost(ctx, mod, streamStatus, uint64(h), 0)
	}
	if r != 1 {
		t.Fatalf("status = %d", r)
	}
	done := make(chan int32)
	go func() {
		var r int32
		for r = -3; r == -3; {
			r = callHost(ctx, mod, streamNext, uint64(h), 100, 64)
		}
		done <- r
	}()
	time.Sleep(20 * time.Millisecond)
	inst.abortHostEffects()
	if r := <-done; r != -2 {
		t.Fatalf("aborted next = %d", r)
	}
	if r := callHost(ctx, mod, streamStatus, uint64(h), 0); r != 1 {
		// The response had settled before the abort, as in libfx.
		t.Fatalf("status after abort = %d", r)
	}
	select {
	case <-body.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("aborted stream did not close its body")
	}
}

func TestHTTPRequestBuffered(t *testing.T) {
	mod := memoryGuest(t)
	inst, ctx := testInstance(t)
	block := make(chan struct{})
	inst.fetch = func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/err":
			return nil, errors.New("boom")
		case "/block":
			select {
			case <-block:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("0123456789")), Header: http.Header{}}, nil
	}
	call := func(path string, capacity uint32) int32 {
		args := putRequestArgs(t, mod, "GET", "https://ai-gateway.vercel.sh"+path, "", "")
		return callHost(ctx, mod, httpRequest, append(args, 0, 100, uint64(capacity))...)
	}
	writeMem(t, mod, 0, []byte{0, 0})
	if r := call("/ok", 64); r != 10 || string(readMem(t, mod, 100, 10)) != "0123456789" {
		t.Fatalf("request = %d", r)
	}
	if v, _ := mod.Memory().ReadUint16Le(0); v != 404 {
		t.Fatalf("status %d", v)
	}
	writeMem(t, mod, 0, []byte{0, 0})
	if r := call("/ok", 4); r != -2 {
		t.Fatalf("too small = %d", r)
	}
	if v, _ := mod.Memory().ReadUint16Le(0); v != 404 {
		t.Fatal("status not written before -2")
	}
	if r := call("/err", 64); r != -1 {
		t.Fatalf("error = %d", r)
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		inst.abortHostEffects()
	}()
	if r := call("/block", 64); r != -1 {
		t.Fatalf("aborted = %d", r)
	}
}

func TestHostToolCall(t *testing.T) {
	mod := memoryGuest(t)
	inst, ctx := testInstance(t)
	toolCall := func(name, args, callID string, capacity uint32) int32 {
		writeMem(t, mod, 4096, []byte(name))
		writeMem(t, mod, 8192, []byte(args))
		writeMem(t, mod, 12288, []byte(callID))
		return callHost(ctx, mod, hostToolCall, 4096, uint64(len(name)), 8192, uint64(len(args)), 100, uint64(capacity), 50, 12288, uint64(len(callID)))
	}
	if r := toolCall("bash", `{}`, "c1", 64); r != -1 {
		t.Fatalf("no executor = %d", r)
	}
	var mu sync.Mutex
	var seen []string
	inst.executeTool = func(ctx context.Context, name string, input json.RawMessage, callID string) (ToolResult, error) {
		mu.Lock()
		seen = append(seen, name+" "+string(input)+" "+callID)
		mu.Unlock()
		switch name {
		case "plain":
			return ToolResult{Content: "ok"}, nil
		case "fail":
			return ToolResult{Content: "bad", IsError: true}, nil
		case "rich":
			return ToolResult{Content: `{"text":"0123456789"}`, Rich: true}, nil
		case "big":
			return ToolResult{Content: strings.Repeat("x", 100)}, nil
		case "err":
			return ToolResult{}, errors.New("boom")
		case "cancelled":
			return ToolResult{Cancelled: true}, nil
		case "panics":
			panic("tool bug")
		case "block":
			<-ctx.Done()
			return ToolResult{Content: "late"}, nil
		}
		return ToolResult{}, nil
	}
	if r := toolCall("plain", `{"a":1}`, "c1", 64); r != 2 || string(readMem(t, mod, 100, 2)) != "ok" || readMem(t, mod, 50, 1)[0] != 0 {
		t.Fatalf("plain = %d", r)
	}
	if seen[0] != `plain {"a":1} c1` {
		t.Fatalf("executor saw %q", seen[0])
	}
	if r := toolCall("fail", `{}`, "c2", 64); r != 3 || readMem(t, mod, 50, 1)[0] != 1 {
		t.Fatalf("isError status = %d %d", r, readMem(t, mod, 50, 1)[0])
	}
	// A rich result over cap is stashed and read in chunks.
	if r := toolCall("rich", `{}`, "c3", 8); r != 21 || readMem(t, mod, 50, 1)[0] != 2 {
		t.Fatalf("rich = %d", r)
	}
	if r := callHost(ctx, mod, hostToolResultRead, 0, 200, 8); r != 8 || string(readMem(t, mod, 200, 8)) != `{"text":` {
		t.Fatalf("read chunk = %d", r)
	}
	if r := callHost(ctx, mod, hostToolResultRead, 16, 200, 8); r != 5 {
		t.Fatalf("tail chunk = %d", r)
	}
	if r := callHost(ctx, mod, hostToolResultRead, 22, 200, 8); r != -1 {
		t.Fatalf("bad offset = %d", r)
	}
	callHost(ctx, mod, fxFuncs["fx_host_tool_result_release"])
	if r := callHost(ctx, mod, hostToolResultRead, 0, 200, 8); r != -1 {
		t.Fatalf("read after release = %d", r)
	}
	if r := toolCall("big", `{}`, "c4", 8); r != -3 {
		t.Fatalf("plain over cap = %d", r)
	}
	if r := toolCall("err", `{}`, "c5", 64); r != -1 {
		t.Fatalf("executor error = %d", r)
	}
	if r := toolCall("panics", `{}`, "c5", 64); r != -1 {
		t.Fatalf("executor panic = %d", r)
	}
	if r := toolCall("cancelled", `{}`, "c6", 64); r != -2 {
		t.Fatalf("cancelled = %d", r)
	}
	if r := toolCall("plain", `{not json`, "c7", 64); r != -1 {
		t.Fatalf("bad args = %d", r)
	}
	// Cancel while the tool blocks: the import returns -2 promptly.
	go func() {
		time.Sleep(30 * time.Millisecond)
		inst.abortHostEffects()
	}()
	if r := toolCall("block", `{}`, "c8", 64); r != -2 {
		t.Fatalf("aborted tool = %d", r)
	}
}

func TestSteering(t *testing.T) {
	mod := memoryGuest(t)
	inst, ctx := testInstance(t)
	e := &Engine{inst: inst}
	if r := callHost(ctx, mod, steeringState); r != -1 {
		t.Fatalf("closed state = %d", r)
	}
	if err := e.Steer("a", "x"); err == nil || err.Error() != "no prompt is running" {
		t.Fatalf("steer while closed: %v", err)
	}
	e.OpenSteering()
	if r := callHost(ctx, mod, steeringTake, 0, 64); r != 0 {
		t.Fatalf("take empty = %d", r)
	}
	if err := e.Steer("id1", "hello"); err != nil {
		t.Fatal(err)
	}
	_ = e.Steer("id2", "again")
	if r := callHost(ctx, mod, steeringState); r != 4 {
		t.Fatalf("state = %d, want 2 queued x 2", r)
	}
	if !e.HandOffSteering() {
		t.Fatal("handoff refused")
	}
	if r := callHost(ctx, mod, steeringState); r != 5 {
		t.Fatalf("state with handoff = %d", r)
	}
	if r := callHost(ctx, mod, steeringTake, 0, 4); r != -1 {
		t.Fatalf("short buffer = %d", r)
	}
	if r := callHost(ctx, mod, steeringTake, 0, 64); r != 9 || string(readMem(t, mod, 0, 9)) != "id1\nhello" {
		t.Fatalf("take = %d %q", r, readMem(t, mod, 0, 9))
	}
	if !e.WithdrawSteering("id2") || e.WithdrawSteering("id2") {
		t.Fatal("withdraw")
	}
	if err := e.Steer("e", ""); err == nil {
		t.Fatal("empty steer accepted")
	}
	if err := e.Steer("big", strings.Repeat("x", maxSteeringMessageBytes+1)); err == nil {
		t.Fatal("oversized steer accepted")
	}
	for i := 0; i < maxSteeringMessages; i++ {
		_ = e.Steer("n", "x")
	}
	if err := e.Steer("n", "x"); err == nil || err.Error() != "steering queue is full" {
		t.Fatalf("full queue: %v", err)
	}
	callHost(ctx, mod, fxFuncs["fx_steering_close"])
	if r := callHost(ctx, mod, steeringState); r != -1 {
		t.Fatalf("state after close = %d", r)
	}
	if r := callHost(ctx, mod, steeringTake, 0, 64); r != 0 {
		t.Fatalf("take after close = %d", r)
	}
}

func TestAttachments(t *testing.T) {
	mod := memoryGuest(t)
	inst, ctx := testInstance(t)
	e := &Engine{inst: inst}
	if r := callHost(ctx, mod, attachmentSize, 1); r != -1 {
		t.Fatalf("missing size = %d", r)
	}
	_ = e.WriteAttachment(1, []byte("payload"))
	_ = e.WriteAttachment(2, nil)
	if r := callHost(ctx, mod, attachmentSize, 1); r != 7 {
		t.Fatalf("size = %d", r)
	}
	if r := callHost(ctx, mod, attachmentTake, 1, 0, 3); r != -1 {
		t.Fatalf("short take = %d", r)
	}
	if r := callHost(ctx, mod, attachmentTake, 1, 0, 64); r != 7 || string(readMem(t, mod, 0, 7)) != "payload" {
		t.Fatalf("take = %d", r)
	}
	if r := callHost(ctx, mod, attachmentTake, 1, 0, 64); r != -1 {
		t.Fatalf("take twice = %d", r)
	}
	// An empty payload needs no valid buffer.
	if r := callHost(ctx, mod, attachmentTake, 2, 1<<30, 0); r != 0 {
		t.Fatalf("empty take = %d", r)
	}
	for i := uint32(10); i < 10+maxPendingAttachments; i++ {
		if err := e.WriteAttachment(i, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.WriteAttachment(99, []byte("x")); err == nil {
		t.Fatal("inbound table overflow accepted")
	}
	e.DiscardAttachments()
	if r := callHost(ctx, mod, attachmentSize, 10); r != -1 {
		t.Fatal("discard kept attachments")
	}

	writeMem(t, mod, 0, []byte("ckpt"))
	var ids []int32
	for i := 0; i < maxOutboundAttachments; i++ {
		id := callHost(ctx, mod, attachmentPut, 0, 4)
		if id < 1 {
			t.Fatalf("put %d = %d", i, id)
		}
		ids = append(ids, id)
	}
	if r := callHost(ctx, mod, attachmentPut, 0, 4); r != -1 {
		t.Fatalf("full outbound table = %d", r)
	}
	if got := e.TakeAttachment(ids[0]); string(got) != "ckpt" {
		t.Fatalf("take outbound %q", got)
	}
	if e.TakeAttachment(ids[0]) != nil {
		t.Fatal("outbound taken twice")
	}
	if r := callHost(ctx, mod, attachmentPut, 1<<30, 4); r != -1 {
		t.Fatalf("out-of-range put = %d", r)
	}
}

func TestUnconfiguredStoresAreUnavailable(t *testing.T) {
	mod := memoryGuest(t)
	_, ctx := testInstance(t)
	for _, name := range []string{"fx_journal_flush", "fx_session_load", "fx_session_commit", "fx_session_list", "fx_session_remove", "fx_oauth_session_load", "fx_oauth_session_commit", "fx_oauth_session_remove"} {
		if r := callHost(ctx, mod, fxFuncs[name], 0, 0, 0, 0, 0, 0, 0, 0, 0); r != -1 {
			t.Errorf("%s = %d, want -1", name, r)
		}
	}
}

func TestAllowlistFetch(t *testing.T) {
	var hit bool
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		hit = true
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
	})}
	fetch := AllowlistFetch(client, DefaultAllowedOrigins)
	for _, u := range []string{"https://evil.example/x", "http://ai-gateway.vercel.sh/x", "https://ai-gateway.vercel.sh.evil.example/x", "https://169.254.169.254/latest"} {
		req, _ := http.NewRequest("GET", u, nil)
		if _, err := fetch(req); err == nil {
			t.Errorf("%s allowed", u)
		}
	}
	if hit {
		t.Fatal("refused request reached the transport")
	}
	req, _ := http.NewRequest("POST", "https://ai-gateway.vercel.sh/v1/ai/language-model", nil)
	resp, err := fetch(req)
	if err != nil || !hit {
		t.Fatalf("gateway refused: %v", err)
	}
	resp.Body.Close() //nolint:errcheck
}

// An allowlist entry with a path allows that endpoint only: the grok
// core's GET https://x.ai/cli/stable must not open all of https://x.ai,
// directly or through a redirect.
func TestAllowlistFetchScopesEndpointEntries(t *testing.T) {
	var hits []string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		hits = append(hits, req.URL.String())
		if req.URL.Path == "/cli/stable" && req.URL.Query().Get("redirect") != "" {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {req.URL.Query().Get("redirect")}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("1.0.13\n")), Header: http.Header{}, Request: req}, nil
	})}
	fetch := AllowlistFetch(client, DefaultAllowedOrigins)
	for _, u := range []string{"https://x.ai/", "https://x.ai/api/keys", "https://x.ai/cli/stable/extra", "https://x.ai/cli", "https://x.ai/cli/stable%2F..%2Fother", "http://x.ai/cli/stable", "https://x.ai:8443/cli/stable"} {
		req, _ := http.NewRequest("GET", u, nil)
		if _, err := fetch(req); err == nil {
			t.Errorf("%s allowed", u)
		}
	}
	if len(hits) != 0 {
		t.Fatalf("refused requests reached the transport: %v", hits)
	}
	req, _ := http.NewRequest("GET", "https://x.ai/cli/stable", nil)
	resp, err := fetch(req)
	if err != nil {
		t.Fatalf("cli/stable refused: %v", err)
	}
	resp.Body.Close() //nolint:errcheck
	req, _ = http.NewRequest("GET", "https://x.ai/cli/stable?redirect=https://x.ai/other", nil)
	if resp, err := fetch(req); err == nil {
		resp.Body.Close() //nolint:errcheck
		t.Fatal("a redirect off the allowed endpoint was followed")
	}
	if len(hits) != 2 {
		t.Fatalf("requests sent = %v", hits)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A custom Fetch that returns (nil, nil) fails the call instead of
// dereferencing a nil response on a host goroutine (a process crash).
func TestHTTPFetchReturningNilResponse(t *testing.T) {
	mod := memoryGuest(t)
	inst, ctx := testInstance(t)
	inst.fetch = func(*http.Request) (*http.Response, error) { return nil, nil }
	args := putRequestArgs(t, mod, "GET", "https://ai-gateway.vercel.sh/x", "", "")
	if r := callHost(ctx, mod, httpRequest, append(args, 0, 100, 64)...); r != -1 {
		t.Fatalf("buffered request = %d, want -1", r)
	}
	h := callHost(ctx, mod, streamOpen, args...)
	deadline := time.Now().Add(5 * time.Second)
	for {
		r := callHost(ctx, mod, streamStatus, uint64(h), 0)
		if r == -1 {
			break
		}
		if r != 0 || time.Now().After(deadline) {
			t.Fatalf("stream status = %d, want -1", r)
		}
	}
	callHost(ctx, mod, streamClose, uint64(h))
}

// The buffered response buffer must lie in guest memory; a capacity past
// its end fails before any request is sent, so the host never buffers up
// to 4 GiB for a guest that cannot hold it.
func TestHTTPRequestRejectsOutOfRangeBuffer(t *testing.T) {
	mod := memoryGuest(t)
	inst, ctx := testInstance(t)
	called := false
	inst.fetch = func(*http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("x")), Header: http.Header{}}, nil
	}
	args := putRequestArgs(t, mod, "GET", "https://ai-gateway.vercel.sh/x", "", "")
	if r := callHost(ctx, mod, httpRequest, append(args, 0, 100, 0xffffffff)...); r != -1 {
		t.Fatalf("request = %d, want -1", r)
	}
	if called {
		t.Fatal("fetch ran for an out-of-range response buffer")
	}
}

// A redirect from an allowed origin must not take the request (and its
// headers) to an origin outside the allowlist.
func TestAllowlistFetchRefusesRedirectOutsideAllowlist(t *testing.T) {
	var hosts []string
	var mu sync.Mutex
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		hosts = append(hosts, r.URL.Host)
		mu.Unlock()
		if r.URL.Host == "ai-gateway.vercel.sh" {
			to := "https://evil.example/collect"
			if r.URL.Path == "/inside" {
				to = "https://chatgpt.com/ok"
			}
			return &http.Response{StatusCode: 307, Header: http.Header{"Location": {to}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
	})}
	fetch := AllowlistFetch(client, DefaultAllowedOrigins)
	req, _ := http.NewRequest("GET", "https://ai-gateway.vercel.sh/x", nil)
	if resp, err := fetch(req); err == nil {
		resp.Body.Close() //nolint:errcheck
		t.Error("redirect to evil.example was followed")
	}
	req, _ = http.NewRequest("GET", "https://ai-gateway.vercel.sh/inside", nil)
	resp, err := fetch(req)
	if err != nil {
		t.Fatalf("redirect inside the allowlist refused: %v", err)
	}
	resp.Body.Close() //nolint:errcheck
	mu.Lock()
	defer mu.Unlock()
	for _, h := range hosts {
		if h == "evil.example" {
			t.Errorf("request reached %s", h)
		}
	}
	if client.CheckRedirect != nil {
		t.Error("AllowlistFetch modified the caller's client")
	}
}

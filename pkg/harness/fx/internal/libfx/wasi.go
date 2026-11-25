package libfx

import (
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"time"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/sys"
)

// WASI errno values the host returns (fx-sdk.js createRuntime wasi table).
const (
	errnoSuccess = 0
	errnoBadf    = 8
	errnoFault   = 21
	errnoSpipe   = 29
	errnoNosys   = 52
)

const wasiModule = "wasi_snapshot_preview1"

var (
	processEpoch     = time.Now()
	processEpochWall = processEpoch.UnixNano()
)

// hostFunc is one host import. Its wasm signature comes from expectedImports.
type hostFunc func(ctx context.Context, mod api.Module, stack []uint64)

func u32(v uint64) uint32 { return api.DecodeU32(v) }

func ret(stack []uint64, v int32) { stack[0] = api.EncodeI32(v) }

func constant(v int32) hostFunc {
	return func(_ context.Context, _ api.Module, stack []uint64) { ret(stack, v) }
}

// wasiFuncs is libfx's own minimal wasi_snapshot_preview1: argv and environ
// from the engine, stdin from the host queue, stdout to the ACP decoder,
// stderr to diagnostics, clocks, randomness, and no filesystem at all (no
// preopens; every path and file call is ENOSYS).
var wasiFuncs = map[string]hostFunc{
	"args_sizes_get":    func(ctx context.Context, mod api.Module, s []uint64) { sizesGet(mod, s, instanceFrom(ctx).args) },
	"args_get":          func(ctx context.Context, mod api.Module, s []uint64) { vectorGet(mod, s, instanceFrom(ctx).args) },
	"environ_sizes_get": func(ctx context.Context, mod api.Module, s []uint64) { sizesGet(mod, s, instanceFrom(ctx).env) },
	"environ_get":       func(ctx context.Context, mod api.Module, s []uint64) { vectorGet(mod, s, instanceFrom(ctx).env) },
	"fd_write":          fdWrite,
	"fd_read":           fdRead,
	"fd_close":          constant(errnoSuccess),
	"fd_fdstat_get":     fdFdstatGet,
	"fd_sync":           constant(errnoSuccess),
	"fd_seek":           constant(errnoSpipe),
	"fd_prestat_get":    constant(errnoBadf),
	"clock_res_get": func(_ context.Context, mod api.Module, s []uint64) {
		if !memWriteU64(mod, u32(s[1]), 1_000_000) {
			ret(s, errnoFault)
			return
		}
		ret(s, errnoSuccess)
	},
	"clock_time_get": clockTimeGet,
	"random_get": func(_ context.Context, mod api.Module, s []uint64) {
		buf, ok := memBytes(mod, u32(s[0]), u32(s[1]))
		if !ok {
			ret(s, errnoFault)
			return
		}
		if _, err := rand.Read(buf); err != nil {
			panic(fmt.Errorf("random_get: %w", err))
		}
		ret(s, errnoSuccess)
	},
	"poll_oneoff": pollOneoff,
	"proc_exit":   procExit,

	"fd_filestat_get":         constant(errnoNosys),
	"fd_filestat_set_size":    constant(errnoNosys),
	"fd_filestat_set_times":   constant(errnoNosys),
	"fd_pread":                constant(errnoNosys),
	"fd_pwrite":               constant(errnoNosys),
	"fd_readdir":              constant(errnoNosys),
	"fd_prestat_dir_name":     constant(errnoNosys),
	"path_create_directory":   constant(errnoNosys),
	"path_filestat_get":       constant(errnoNosys),
	"path_filestat_set_times": constant(errnoNosys),
	"path_link":               constant(errnoNosys),
	"path_open":               constant(errnoNosys),
	"path_readlink":           constant(errnoNosys),
	"path_remove_directory":   constant(errnoNosys),
	"path_rename":             constant(errnoNosys),
	"path_symlink":            constant(errnoNosys),
	"path_unlink_file":        constant(errnoNosys),
}

func sizesGet(mod api.Module, s []uint64, values []string) {
	size := 0
	for _, v := range values {
		size += len(v) + 1
	}
	if !memWriteU32(mod, u32(s[0]), uint32(len(values))) || !memWriteU32(mod, u32(s[1]), uint32(size)) {
		ret(s, errnoFault)
		return
	}
	ret(s, errnoSuccess)
}

// vectorGet writes NUL-terminated strings at data and their pointers at ptrs.
func vectorGet(mod api.Module, s []uint64, values []string) {
	ptrs, cursor := u32(s[0]), u32(s[1])
	for i, v := range values {
		if !memWriteU32(mod, ptrs+uint32(i)*4, cursor) || !memWrite(mod, cursor, append([]byte(v), 0)) {
			ret(s, errnoFault)
			return
		}
		cursor += uint32(len(v)) + 1
	}
	ret(s, errnoSuccess)
}

// fdWrite gathers iovecs (8 bytes each: ptr u32, len u32). fd 1 feeds the
// ACP decoder and may block for backpressure; fd 2 goes to diagnostics;
// other fds are accepted and dropped.
func fdWrite(ctx context.Context, mod api.Module, s []uint64) {
	inst := instanceFrom(ctx)
	fd, iovs, count, nwritten := u32(s[0]), u32(s[1]), u32(s[2]), u32(s[3])
	if count > math.MaxUint32/8 {
		ret(s, errnoFault)
		return
	}
	vec, ok := memBytes(mod, iovs, count*8)
	if !ok {
		ret(s, errnoFault)
		return
	}
	var total uint64
	for i := uint32(0); i < count; i++ {
		total += uint64(le32(vec[i*8+4:]))
	}
	if fd == 1 && total > MaxCoreMessageBytes {
		panic(fmt.Errorf("core output message exceeds 64 MiB"))
	}
	if fd == 1 || fd == 2 {
		chunk := make([]byte, 0, total)
		for i := uint32(0); i < count; i++ {
			b, ok := memBytes(mod, le32(vec[i*8:]), le32(vec[i*8+4:]))
			if !ok {
				ret(s, errnoFault)
				return
			}
			chunk = append(chunk, b...)
		}
		if fd == 1 {
			if err := inst.out.write(chunk); err != nil {
				// The JS host throws here, which aborts the runtime.
				panic(err)
			}
		} else if inst.stderr != nil {
			_, _ = inst.stderr.Write(chunk)
		}
	}
	if !memWriteU32(mod, nwritten, uint32(total)) {
		ret(s, errnoFault)
		return
	}
	ret(s, errnoSuccess)
}

// fdRead fills iovecs from stdin (fd 0 only). An empty queue blocks until a
// push or close; a closed queue reads as EOF (nread = 0).
func fdRead(ctx context.Context, mod api.Module, s []uint64) {
	inst := instanceFrom(ctx)
	fd, iovs, count, nread := u32(s[0]), u32(s[1]), u32(s[2]), u32(s[3])
	if fd != 0 {
		ret(s, errnoBadf)
		return
	}
	if count > math.MaxUint32/8 {
		ret(s, errnoFault)
		return
	}
	attempt := func() (uint32, bool) {
		vec, ok := memBytes(mod, iovs, count*8)
		if !ok {
			return 0, false
		}
		var total uint32
		for i := uint32(0); i < count; i++ {
			ptr, length := le32(vec[i*8:]), le32(vec[i*8+4:])
			if _, ok := memBytes(mod, ptr, length); !ok {
				return 0, false
			}
			chunk := inst.stdin.read(int(length))
			if chunk == nil {
				break
			}
			memWrite(mod, ptr, chunk)
			total += uint32(len(chunk))
			if uint32(len(chunk)) < length {
				break
			}
		}
		return total, true
	}
	finish := func(n uint32) {
		if !memWriteU32(mod, nread, n) {
			ret(s, errnoFault)
			return
		}
		ret(s, errnoSuccess)
	}
	n, ok := attempt()
	if !ok {
		ret(s, errnoFault)
		return
	}
	if n > 0 || inst.stdin.isClosed() {
		finish(n)
		return
	}
	inst.stdin.wait(inst.runCtx, -1)
	n, ok = attempt()
	if !ok {
		ret(s, errnoFault)
		return
	}
	finish(n)
}

// pollOneoff implements the core's only timer. Subscriptions are 48 bytes
// (userdata u64 at 0, tag u8 at 8, clock timeout u64 ns at 24); events are
// 32 bytes (userdata at 0, type u8 at 10). A ready fd_read subscription
// returns at once; otherwise it waits for a stdin wake or the clock timeout
// and reports the first subscription's userdata, exactly as libfx does.
func pollOneoff(ctx context.Context, mod api.Module, s []uint64) {
	inst := instanceFrom(ctx)
	in, out, count, nevents := u32(s[0]), u32(s[1]), u32(s[2]), u32(s[3])
	if count == 0 || count > math.MaxUint32/48 {
		ret(s, errnoFault)
		return
	}
	subs, ok := memBytes(mod, in, count*48)
	if !ok {
		ret(s, errnoFault)
		return
	}
	subs = append([]byte(nil), subs...)
	writeEvent := func(userdata []byte, eventType byte) {
		ev := make([]byte, 32)
		copy(ev, userdata)
		ev[10] = eventType
		if !memWrite(mod, out, ev) || !memWriteU32(mod, nevents, 1) {
			ret(s, errnoFault)
			return
		}
		ret(s, errnoSuccess)
	}
	for i := uint32(0); i < count; i++ {
		base := subs[i*48:]
		if base[8] == 1 && inst.stdin.hasData() {
			writeEvent(base[:8], 1)
			return
		}
	}
	timeout := time.Duration(-1)
	for i := uint32(0); i < count; i++ {
		base := subs[i*48:]
		if base[8] == 0 {
			ns := uint64(le32(base[24:])) | uint64(le32(base[28:]))<<32
			if ns > math.MaxInt64 {
				timeout = -1
			} else {
				timeout = time.Duration(ns)
			}
		}
	}
	inst.stdin.wait(inst.runCtx, timeout)
	writeEvent(subs[:8], 0)
}

// clockTimeGet: realtime ns for clock 0; for the monotonic clock (1), the
// process start wall time plus monotonic elapsed time, so it never goes
// backwards. libfx JS returns wall time for every clock id.
func clockTimeGet(_ context.Context, mod api.Module, s []uint64) {
	var now int64
	if u32(s[0]) == 1 {
		now = processEpochWall + int64(time.Since(processEpoch))
	} else {
		now = time.Now().UnixNano()
	}
	if !memWriteU64(mod, u32(s[2]), uint64(now)) {
		ret(s, errnoFault)
		return
	}
	ret(s, errnoSuccess)
}

func fdFdstatGet(_ context.Context, mod api.Module, s []uint64) {
	fd, out := u32(s[0]), u32(s[1])
	stat := make([]byte, 24)
	if fd <= 2 {
		stat[0] = 2 // character device
	}
	for i := 8; i < 24; i++ {
		stat[i] = 0xff
	}
	if !memWrite(mod, out, stat) {
		ret(s, errnoFault)
		return
	}
	ret(s, errnoSuccess)
}

// procExit records the code, closes the guest and unwinds _start, the same
// pattern wazero's own WASI module uses.
func procExit(ctx context.Context, mod api.Module, s []uint64) {
	inst := instanceFrom(ctx)
	code := u32(s[0])
	inst.mu.Lock()
	inst.exitCode = int32(code)
	inst.exited = true
	inst.mu.Unlock()
	_ = mod.CloseWithExitCode(ctx, code)
	panic(sys.NewExitError(code))
}

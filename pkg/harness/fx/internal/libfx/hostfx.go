package libfx

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

const fxModule = "fx"

// fxFuncs is the "fx" host module fx-core imports (fx-sdk.js createRuntime
// fx table, minus the fx-term-only imports fx-core never declares).
var fxFuncs = map[string]hostFunc{
	"fx_http_stream_open":   streamOpen,
	"fx_http_stream_status": streamStatus,
	"fx_http_stream_next":   streamNext,
	"fx_http_stream_close":  streamClose,
	"fx_http_request":       httpRequest,

	"fx_host_tool_call":           hostToolCall,
	"fx_host_tool_result_read":    hostToolResultRead,
	"fx_host_tool_result_release": func(ctx context.Context, _ api.Module, _ []uint64) { instanceFrom(ctx).pendingToolResult = nil },

	"fx_steering_take":  steeringTake,
	"fx_steering_state": steeringState,
	"fx_steering_close": func(ctx context.Context, _ api.Module, _ []uint64) { instanceFrom(ctx).closeSteering() },

	"fx_attachment_size": attachmentSize,
	"fx_attachment_take": attachmentTake,
	"fx_attachment_put":  attachmentPut,

	// No journal, session store or OAuth session store is configured: the
	// engine path the harness uses keeps state in checkpoints. -1 is the
	// "unavailable" answer for each (fx-sdk.js:1213, :1263-1299, :1375-1415).
	"fx_journal_flush":        constant(-1),
	"fx_session_load":         constant(-1),
	"fx_session_commit":       constant(-1),
	"fx_session_list":         constant(-1),
	"fx_session_remove":       constant(-1),
	"fx_oauth_session_load":   constant(-1),
	"fx_oauth_session_commit": constant(-1),
	"fx_oauth_session_remove": constant(-1),
}

// hostFuncFor returns the implementation of one pinned import.
func hostFuncFor(module, name string) (hostFunc, bool) {
	switch module {
	case wasiModule:
		f, ok := wasiFuncs[name]
		return f, ok
	case fxModule:
		f, ok := fxFuncs[name]
		return f, ok
	}
	return nil, false
}

// instantiateHostModules defines every pinned import, with its pinned
// signature, in rt. The functions reach engine state through the call ctx,
// so one pair of host modules serves every guest in the runtime.
func instantiateHostModules(ctx context.Context, rt wazero.Runtime) error {
	builders := map[string]wazero.HostModuleBuilder{
		wasiModule: rt.NewHostModuleBuilder(wasiModule),
		fxModule:   rt.NewHostModuleBuilder(fxModule),
	}
	for _, sig := range expectedImports {
		fn, ok := hostFuncFor(sig.Module, sig.Name)
		b := builders[sig.Module]
		if !ok || b == nil {
			return fmt.Errorf("fx runtime: the host does not implement %s.%s", sig.Module, sig.Name)
		}
		b.NewFunctionBuilder().
			WithGoModuleFunction(api.GoModuleFunc(fn), sig.Params, sig.Results).
			Export(sig.Name)
	}
	for _, name := range []string{wasiModule, fxModule} {
		if _, err := builders[name].Instantiate(ctx); err != nil {
			return fmt.Errorf("fx runtime: host module %s: %w", name, err)
		}
	}
	return nil
}

// hostToolCall runs a host tool and blocks until it settles. Returns the
// output length (<= cap), -1 failure, -2 cancelled, -3 too large. The status
// byte has bit0 for isError and bit1 for a rich result; a rich result over
// cap (up to 8 MiB) is stashed for fx_host_tool_result_read and its full
// length returned.
func hostToolCall(ctx context.Context, mod api.Module, s []uint64) {
	inst := instanceFrom(ctx)
	inst.pendingToolResult = nil
	if inst.executeTool == nil {
		ret(s, -1)
		return
	}
	name, ok1 := memString(mod, u32(s[0]), u32(s[1]))
	args, ok2 := memCopy(mod, u32(s[2]), u32(s[3]))
	outPtr, outCap, statusPtr := u32(s[4]), u32(s[5]), u32(s[6])
	callID, ok3 := memString(mod, u32(s[7]), u32(s[8]))
	if !ok1 || !ok2 || !ok3 || !json.Valid(args) {
		ret(s, -1)
		return
	}
	ectx, release := inst.newEffect()
	defer release()
	type outcome struct {
		res ToolResult
		err error
	}
	// The tool runs on its own goroutine so an abort returns -2 at once; a
	// tool that ignores ctx finishes in the background and its result is
	// dropped.
	done := make(chan outcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- outcome{err: fmt.Errorf("host tool %s panicked: %v", name, r)}
			}
		}()
		res, err := inst.executeTool(ectx, name, json.RawMessage(args), callID)
		done <- outcome{res, err}
	}()
	var o outcome
	select {
	case o = <-done:
	case <-ectx.Done():
		ret(s, -2)
		return
	}
	if o.err != nil {
		ret(s, -1)
		return
	}
	if o.res.Cancelled || ectx.Err() != nil {
		ret(s, -2)
		return
	}
	output := []byte(o.res.Content)
	var status byte
	if o.res.IsError {
		status |= 1
	}
	if o.res.Rich {
		status |= 2
	}
	if !memWrite(mod, statusPtr, []byte{status}) {
		ret(s, -1)
		return
	}
	if uint64(len(output)) > uint64(outCap) {
		if !o.res.Rich || len(output) > maxRichToolResultBytes {
			ret(s, -3)
			return
		}
		inst.pendingToolResult = output
		ret(s, int32(len(output)))
		return
	}
	if !memWrite(mod, outPtr, output) {
		ret(s, -1)
		return
	}
	ret(s, int32(len(output)))
}

func hostToolResultRead(ctx context.Context, mod api.Module, s []uint64) {
	inst := instanceFrom(ctx)
	offset, ptr, capacity := api.DecodeI32(s[0]), u32(s[1]), u32(s[2])
	pending := inst.pendingToolResult
	if pending == nil || offset < 0 || int(offset) > len(pending) {
		ret(s, -1)
		return
	}
	chunk := pending[offset:]
	if uint64(len(chunk)) > uint64(capacity) {
		chunk = chunk[:capacity]
	}
	if !memWrite(mod, ptr, chunk) {
		ret(s, -1)
		return
	}
	ret(s, int32(len(chunk)))
}

// steeringTake pops one steer framed as "id\ntext". 0 none waiting, -1
// buffer too small.
func steeringTake(ctx context.Context, mod api.Module, s []uint64) {
	inst := instanceFrom(ctx)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if len(inst.steering) == 0 {
		ret(s, 0)
		return
	}
	entry := inst.steering[0]
	frame := make([]byte, 0, len(entry.id)+1+len(entry.value))
	frame = append(frame, entry.id...)
	frame = append(frame, '\n')
	frame = append(frame, entry.value...)
	outPtr, outCap := u32(s[0]), u32(s[1])
	if _, ok := memBytes(mod, outPtr, outCap); !ok || uint64(len(frame)) > uint64(outCap) {
		ret(s, -1)
		return
	}
	memWrite(mod, outPtr, frame)
	inst.steering = inst.steering[1:]
	inst.steeringBytes -= len(entry.value)
	ret(s, int32(len(frame)))
}

// steeringState: -1 once steering is closed, else 2 x queued + handoff.
func steeringState(ctx context.Context, _ api.Module, s []uint64) {
	inst := instanceFrom(ctx)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if !inst.steeringOpen {
		ret(s, -1)
		return
	}
	v := int32(len(inst.steering) * 2)
	if inst.steeringHandoff {
		v++
	}
	ret(s, v)
}

func (inst *instance) closeSteering() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.steeringOpen = false
	inst.clearSteeringLocked()
}

func (inst *instance) clearSteeringLocked() {
	inst.steering = nil
	inst.steeringBytes = 0
	inst.steeringHandoff = false
}

func attachmentSize(ctx context.Context, _ api.Module, s []uint64) {
	inst := instanceFrom(ctx)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	v, ok := inst.inbound[u32(s[0])]
	if !ok {
		ret(s, -1)
		return
	}
	ret(s, int32(len(v)))
}

// attachmentTake copies an inbound payload and deletes it; -1 on a miss or
// a short buffer.
func attachmentTake(ctx context.Context, mod api.Module, s []uint64) {
	inst := instanceFrom(ctx)
	id, ptr, capacity := u32(s[0]), u32(s[1]), u32(s[2])
	inst.mu.Lock()
	defer inst.mu.Unlock()
	v, ok := inst.inbound[id]
	if !ok {
		ret(s, -1)
		return
	}
	// An empty payload needs no output buffer, whose pointer may be arbitrary.
	if len(v) > 0 {
		if _, ok := memBytes(mod, ptr, capacity); !ok || uint64(len(v)) > uint64(capacity) {
			ret(s, -1)
			return
		}
		memWrite(mod, ptr, v)
	}
	delete(inst.inbound, id)
	ret(s, int32(len(v)))
}

// attachmentPut publishes outbound bytes and returns an id >= 1, or -1 when
// the table (4 slots) is full.
func attachmentPut(ctx context.Context, mod api.Module, s []uint64) {
	inst := instanceFrom(ctx)
	data, ok := memCopy(mod, u32(s[0]), u32(s[1]))
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if !ok || len(inst.outbound) >= maxOutboundAttachments {
		ret(s, -1)
		return
	}
	id := inst.nextOutbound
	if id == 0x7fffffff {
		inst.nextOutbound = 1
	} else {
		inst.nextOutbound = id + 1
	}
	inst.outbound[id] = data
	ret(s, id)
}

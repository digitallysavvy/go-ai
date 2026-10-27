package libfx

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/tetratelabs/wazero/api"
)

// libfx byte and count limits (fx-sdk.js:15-63) that the host enforces.
const (
	maxSteeringMessageBytes = 64 << 10
	maxSteeringMessages     = 64
	maxSteeringQueueBytes   = 1 << 20
	maxPendingAttachments   = 8
	maxOutboundAttachments  = 4
	maxRichToolResultBytes  = 8 << 20
	// MaxCheckpointBytes matches the core's max_checkpoint_bytes.
	MaxCheckpointBytes = 4 << 20
	// MaxPromptFrameBytes is the largest ACP frame the core accepts.
	MaxPromptFrameBytes = 8 << 20
)

// ToolResult is a host tool's answer to the core (fx-sdk.js executeHostTool).
type ToolResult struct {
	Content string
	IsError bool
	// Rich marks Content as JSON {text, images}.
	Rich bool
	// Cancelled reports that the call was abandoned; the core sees -2.
	Cancelled bool
}

// ToolExecutor runs one host tool. ctx is cancelled when host effects are
// aborted. An error is reported to the core as a failed call (-1).
type ToolExecutor func(ctx context.Context, name string, input json.RawMessage, callID string) (ToolResult, error)

// FetchFunc performs one HTTP request for the core (model calls, the model
// catalog, OAuth). The request carries the effect ctx.
type FetchFunc func(*http.Request) (*http.Response, error)

type instanceKey struct{}

// instance is the per-engine host state the shared host functions reach
// through the call ctx. Linear memory is touched only on the guest
// goroutine, inside host function calls.
type instance struct {
	args []string
	env  []string

	stdin  *byteQueue
	out    *coreOutput
	stderr io.Writer

	// runCtx ends with the engine; blocking host calls also watch it.
	runCtx context.Context

	fetch       FetchFunc
	executeTool ToolExecutor

	mu         sync.Mutex
	effects    map[int]context.CancelFunc
	nextEffect int
	streams    map[int32]*stream
	nextHandle int32

	steering        []steer
	steeringBytes   int
	steeringOpen    bool
	steeringHandoff bool

	inbound      map[uint32][]byte
	outbound     map[int32][]byte
	nextOutbound int32

	// pendingToolResult is touched only on the guest goroutine.
	pendingToolResult []byte

	exitCode int32
	exited   bool
}

type steer struct {
	id    string
	value []byte
}

func instanceFrom(ctx context.Context) *instance {
	inst, _ := ctx.Value(instanceKey{}).(*instance)
	if inst == nil {
		panic(errors.New("fx host function called outside an fx engine"))
	}
	return inst
}

// newEffect returns a ctx cancelled by abortHostEffects or engine end, and
// a release func the caller defers.
func (inst *instance) newEffect() (context.Context, func()) {
	ctx, cancel := context.WithCancel(inst.runCtx)
	inst.mu.Lock()
	id := inst.nextEffect
	inst.nextEffect++
	inst.effects[id] = cancel
	inst.mu.Unlock()
	return ctx, func() {
		inst.mu.Lock()
		delete(inst.effects, id)
		inst.mu.Unlock()
		cancel()
	}
}

// abortHostEffects cancels every in-flight stream, request and tool call;
// the blocked imports return their aborted or cancelled codes.
func (inst *instance) abortHostEffects() {
	inst.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(inst.effects))
	for _, c := range inst.effects {
		cancels = append(cancels, c)
	}
	inst.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// Memory helpers. Every guest pointer is bounds-checked; out-of-range access
// reports !ok instead of trapping the host.

func memBytes(mod api.Module, ptr, length uint32) ([]byte, bool) {
	return mod.Memory().Read(ptr, length)
}

// memCopy returns a copy of guest bytes, safe to keep after the call.
func memCopy(mod api.Module, ptr, length uint32) ([]byte, bool) {
	b, ok := memBytes(mod, ptr, length)
	if !ok {
		return nil, false
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out, true
}

func memString(mod api.Module, ptr, length uint32) (string, bool) {
	b, ok := memBytes(mod, ptr, length)
	if !ok {
		return "", false
	}
	return string(b), true
}

func memWrite(mod api.Module, ptr uint32, data []byte) bool {
	if len(data) == 0 {
		return true
	}
	return mod.Memory().Write(ptr, data)
}

func memWriteU16(mod api.Module, ptr uint32, v uint16) bool {
	return mod.Memory().WriteUint16Le(ptr, v)
}

func memWriteU32(mod api.Module, ptr uint32, v uint32) bool {
	return mod.Memory().WriteUint32Le(ptr, v)
}

func memWriteU64(mod api.Module, ptr uint32, v uint64) bool {
	return mod.Memory().WriteUint64Le(ptr, v)
}

func memReadU32(mod api.Module, ptr uint32) (uint32, bool) {
	return mod.Memory().ReadUint32Le(ptr)
}

func le32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }

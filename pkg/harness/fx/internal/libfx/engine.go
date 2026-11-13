package libfx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/sys"
)

// closeGrace is how long Close waits for the core to exit after stdin
// closes before stopping it (fx-sdk.js close).
const closeGrace = 5 * time.Second

// Bounded option strings (fx-sdk.js boundedString limits).
const (
	maxAPIKeyBytes = 64 << 10
	maxModelBytes  = 1024
	maxEffortBytes = 64
)

// inboxSize bounds decoded core messages awaiting dispatch; when it is full
// the guest blocks in fd_write.
const inboxSize = 256

// Options configures one engine: one fx-core instance.
type Options struct {
	// Wasm locates fx-core.wasm.
	Wasm Source
	// MemoryLimitPages caps guest memory (64 KiB pages); 0 means
	// DefaultMemoryLimitPages.
	MemoryLimitPages uint32

	// Core configuration, passed as environment variables.
	APIKey    string // AI_GATEWAY_API_KEY
	Provider  string // FX_PROVIDER: gateway, codex, grok or configured
	Model     string // FX_MODEL
	Effort    string // FX_EFFORT
	Fast      *bool  // FX_FAST
	Ultrafast *bool  // FX_ULTRAFAST

	// Fetch performs the core's HTTP requests. nil uses an SSRF-safe client
	// limited to DefaultAllowedOrigins.
	Fetch FetchFunc
	// ExecuteTool runs host tools. nil makes every tool call fail.
	ExecuteTool ToolExecutor
	// OnNotification receives core notifications (session/update,
	// libfx/hook, ...) in order, on one goroutine. A slow handler applies
	// backpressure to the core.
	OnNotification func(method string, params json.RawMessage)
	// OnGuestNotification, when set, receives core notifications instead of
	// OnNotification, synchronously on the guest goroutine inside fd_write,
	// with the size of the line that carried each one. Core output after a
	// notification is not read until the handler returns, which is the
	// ordering and backpressure libfx's suspending fd_write gives a JS host:
	// a notification is handled before the core's next host call. The handler
	// must return once ctx (the engine's run context) ends.
	OnGuestNotification func(ctx context.Context, method string, params json.RawMessage, size int)
	// Stderr receives the core's diagnostics. The last 8 KiB are also kept
	// for error messages.
	Stderr io.Writer
}

// RPCError is a JSON-RPC error answer from the core.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return e.Message }

// ExitError reports that the core exited (or trapped) before answering.
type ExitError struct {
	Code   uint32
	Err    error  // the trap or abort cause; nil for a plain exit
	Stderr string // the tail of the core's diagnostics
}

func (e *ExitError) Error() string {
	msg := fmt.Sprintf("fx-core exited with code %d before completing the ACP request", e.Code)
	if e.Err != nil {
		msg = fmt.Sprintf("fx-core aborted: %v", e.Err)
	}
	if e.Stderr != "" {
		msg += fmt.Sprintf(" (stderr: %q)", e.Stderr)
	}
	return msg
}

func (e *ExitError) Unwrap() error { return e.Err }

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type rpcReply struct {
	result json.RawMessage
	err    error
}

// Engine is one running fx-core. Its methods are safe for concurrent use.
type Engine struct {
	inst      *instance
	mod       api.Module
	opts      Options
	runCancel context.CancelFunc
	stderr    *tailBuffer

	inbox chan rpcMessage
	done  chan struct{} // closed when _start returns
	// dispatched is closed when every core message has been handled and
	// pending requests have been failed.
	dispatched chan struct{}

	mu          sync.Mutex
	nextID      int64
	pending     map[int64]chan rpcReply
	pendingDone bool
	closing     bool
	exitErr     error // nil for a clean exit 0
	exitCode    uint32
}

// Start resolves and compiles fx-core.wasm (downloading it on first use),
// instantiates it and runs `fx acp`. ctx bounds resolution and compilation;
// the engine runs until Close.
func Start(ctx context.Context, opts Options) (*Engine, error) {
	env, err := agentEnvironment(opts)
	if err != nil {
		return nil, err
	}
	mod, err := Resolve(ctx, opts.Wasm)
	if err != nil {
		return nil, err
	}
	h, err := loadHost(ctx, mod, opts.MemoryLimitPages)
	if err != nil {
		return nil, err
	}
	fetch := opts.Fetch
	if fetch == nil {
		fetch = AllowlistFetch(defaultFetchClient, DefaultAllowedOrigins)
	}
	runCtx, runCancel := context.WithCancel(context.WithoutCancel(ctx))
	tail := &tailBuffer{max: 8 << 10}
	var stderr io.Writer = tail
	if opts.Stderr != nil {
		stderr = io.MultiWriter(tail, opts.Stderr)
	}
	e := &Engine{
		opts:       opts,
		runCancel:  runCancel,
		stderr:     tail,
		inbox:      make(chan rpcMessage, inboxSize),
		done:       make(chan struct{}),
		dispatched: make(chan struct{}),
		pending:    map[int64]chan rpcReply{},
	}
	inst := &instance{
		args:         []string{"fx", "acp"},
		env:          env,
		stdin:        newByteQueue(),
		stderr:       stderr,
		runCtx:       runCtx,
		fetch:        fetch,
		executeTool:  e.executeTool,
		effects:      map[int]context.CancelFunc{},
		streams:      map[int32]*stream{},
		inbound:      map[uint32][]byte{},
		outbound:     map[int32][]byte{},
		nextOutbound: 1,
	}
	inst.out = &coreOutput{handle: e.onLine}
	e.inst = inst
	callCtx := context.WithValue(runCtx, instanceKey{}, inst)
	guest, err := h.rt.InstantiateModule(callCtx, h.compiled, wazero.NewModuleConfig().WithName("").WithStartFunctions())
	if err != nil {
		runCancel()
		return nil, fmt.Errorf("fx runtime: instantiate fx-core.wasm: %w", err)
	}
	e.mod = guest
	start := guest.ExportedFunction("_start")
	go e.dispatch()
	go e.run(callCtx, start)
	return e, nil
}

func agentEnvironment(o Options) ([]string, error) {
	check := func(name, v string, max int) error {
		if len(v) > max {
			return fmt.Errorf("fx runtime: %s exceeds %d bytes", name, max)
		}
		return nil
	}
	if err := errors.Join(check("apiKey", o.APIKey, maxAPIKeyBytes), check("model", o.Model, maxModelBytes), check("effort", o.Effort, maxEffortBytes)); err != nil {
		return nil, err
	}
	var env []string
	add := func(k, v string) { env = append(env, k+"="+v) }
	if o.APIKey != "" {
		add("AI_GATEWAY_API_KEY", o.APIKey)
	}
	if o.Provider != "" {
		add("FX_PROVIDER", o.Provider)
	}
	if o.Model != "" {
		add("FX_MODEL", o.Model)
	}
	if o.Effort != "" {
		add("FX_EFFORT", o.Effort)
	}
	if o.Fast != nil {
		add("FX_FAST", strconv.FormatBool(*o.Fast))
	}
	if o.Ultrafast != nil {
		add("FX_ULTRAFAST", strconv.FormatBool(*o.Ultrafast))
	}
	return env, nil
}

// run calls _start on the guest goroutine and records how it ended.
func (e *Engine) run(ctx context.Context, start api.Function) {
	_, err := start.Call(ctx)
	code := uint32(0)
	var cause error
	var exit *sys.ExitError
	switch {
	case err == nil:
		cause = e.inst.out.finish()
	case errors.As(err, &exit):
		code = exit.ExitCode()
		if e.inst.runCtx.Err() != nil && code != 0 {
			cause = errors.New("stopped by the host")
		}
	default:
		code = 1
		cause = err
	}
	e.inst.abortHostEffects()
	e.inst.stdin.close()
	e.mu.Lock()
	e.exitCode = code
	if cause != nil || code != 0 {
		e.exitErr = &ExitError{Code: code, Err: cause, Stderr: e.stderr.String()}
	}
	e.mu.Unlock()
	close(e.inbox)
	close(e.done)
}

// onLine runs on the guest goroutine for each decoded stdout message.
func (e *Engine) onLine(line []byte, size int) error {
	var msg rpcMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return fmt.Errorf("core output is not JSON: %w", err)
	}
	if e.opts.OnGuestNotification != nil && msg.Method != "" && len(msg.ID) == 0 {
		e.opts.OnGuestNotification(e.inst.runCtx, msg.Method, msg.Params, size)
		return e.inst.runCtx.Err()
	}
	select {
	case e.inbox <- msg:
		return nil
	case <-e.inst.runCtx.Done():
		return e.inst.runCtx.Err()
	}
}

// dispatch routes core messages: responses to their requests,
// notifications to OnNotification, and core requests to their handlers.
func (e *Engine) dispatch() {
	defer close(e.dispatched)
	for msg := range e.inbox {
		switch {
		case msg.Method == "" && len(msg.ID) > 0:
			e.resolve(msg)
		case msg.Method != "" && len(msg.ID) == 0:
			if e.opts.OnNotification != nil {
				e.opts.OnNotification(msg.Method, msg.Params)
			}
		case msg.Method != "":
			go e.handleCoreRequest(msg)
		}
	}
	e.mu.Lock()
	err := e.exitErr
	if err == nil {
		err = &ExitError{Code: e.exitCode, Stderr: e.stderr.String()}
	}
	pending := e.pending
	e.pending = nil
	e.pendingDone = true
	e.mu.Unlock()
	for _, ch := range pending {
		ch <- rpcReply{err: err}
	}
}

func (e *Engine) resolve(msg rpcMessage) {
	id, err := strconv.ParseInt(string(msg.ID), 10, 64)
	if err != nil {
		return
	}
	e.mu.Lock()
	ch := e.pending[id]
	delete(e.pending, id)
	e.mu.Unlock()
	if ch == nil {
		return
	}
	if msg.Error != nil {
		ch <- rpcReply{err: msg.Error}
		return
	}
	ch <- rpcReply{result: msg.Result}
}

// handleCoreRequest answers the core's own requests (fx-sdk.js
// handleControlMessage). Unknown requests are dropped, as in libfx.
func (e *Engine) handleCoreRequest(msg rpcMessage) {
	switch msg.Method {
	case "session/request_permission":
		// Approvals are host-side; the core's permission requests are
		// always cancelled (TS passes no onPermission).
		_ = e.send(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}})
	case "libfx/journal_flush":
		_ = e.send(map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": map[string]any{}})
	case "libfx/tool_call":
		var p struct {
			Name       string          `json:"name"`
			Input      json.RawMessage `json:"input"`
			ToolCallID string          `json:"toolCallId"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		ctx, release := e.inst.newEffect()
		res, err := e.executeTool(ctx, p.Name, p.Input, p.ToolCallID)
		aborted := ctx.Err() != nil
		release()
		if err != nil {
			res = ToolResult{Content: err.Error(), IsError: true}
		}
		if res.Cancelled || aborted {
			return
		}
		result := map[string]any{"content": res.Content, "isError": res.IsError}
		if res.Rich {
			result["contentType"] = "rich"
		}
		reply := map[string]any{"jsonrpc": "2.0", "id": msg.ID, "result": result}
		if data, _ := json.Marshal(reply); len(data)+1 > MaxPromptFrameBytes {
			reply["result"] = map[string]any{"content": "Host tool result exceeded the response frame limit", "isError": true}
		}
		_ = e.send(reply)
	}
}

// executeTool wraps Options.ExecuteTool: providers reject an empty error
// result, so one gets a message (fx-sdk.js executeHostTool).
func (e *Engine) executeTool(ctx context.Context, name string, input json.RawMessage, callID string) (ToolResult, error) {
	if e.opts.ExecuteTool == nil {
		return ToolResult{}, errors.New("no host tools are configured")
	}
	res, err := e.opts.ExecuteTool(ctx, name, input, callID)
	if err != nil {
		return res, err
	}
	if res.IsError && res.Content == "" {
		res.Content = fmt.Sprintf("Tool %s failed without a message", name)
	}
	return res, nil
}

// send writes one JSON-RPC frame to the core's stdin.
func (e *Engine) send(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(data)+1 > MaxPromptFrameBytes {
		return fmt.Errorf("fx runtime: ACP frame exceeds %d bytes", MaxPromptFrameBytes)
	}
	return e.inst.stdin.push(append(data, '\n'))
}

// Request sends a JSON-RPC request and waits for the core's answer, ctx, or
// the core's exit. An error answer is an *RPCError; an exit is an
// *ExitError.
func (e *Engine) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if params == nil {
		params = map[string]any{}
	}
	ch := make(chan rpcReply, 1)
	e.mu.Lock()
	if e.closing {
		e.mu.Unlock()
		return nil, errors.New("fx agent is closing")
	}
	if e.pendingDone {
		err := e.exitErr
		e.mu.Unlock()
		if err == nil {
			err = &ExitError{Code: e.ExitCode()}
		}
		return nil, err
	}
	e.nextID++
	id := e.nextID
	e.pending[id] = ch
	e.mu.Unlock()
	if err := e.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		if !errors.Is(err, errStdinClosed) {
			e.forget(id)
			return nil, err
		}
		// The core is exiting; the dispatcher fails the request with the
		// exit error.
	}
	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
		e.forget(id)
		return nil, ctx.Err()
	}
}

func (e *Engine) forget(id int64) {
	e.mu.Lock()
	if e.pending != nil {
		delete(e.pending, id)
	}
	e.mu.Unlock()
}

// Notify sends a JSON-RPC notification (for example session/cancel).
func (e *Engine) Notify(method string, params any) error {
	if params == nil {
		params = map[string]any{}
	}
	return e.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// WriteAttachment places raw bytes beside the next frame, under id.
func (e *Engine) WriteAttachment(id uint32, data []byte) error {
	e.inst.mu.Lock()
	defer e.inst.mu.Unlock()
	if len(e.inst.inbound) >= maxPendingAttachments {
		return errors.New("attachment table is full")
	}
	e.inst.inbound[id] = append([]byte(nil), data...)
	return nil
}

// DiscardAttachments drops inbound attachments no frame consumed.
func (e *Engine) DiscardAttachments() {
	e.inst.mu.Lock()
	defer e.inst.mu.Unlock()
	clear(e.inst.inbound)
}

// TakeAttachment returns and removes an attachment the core published.
func (e *Engine) TakeAttachment(id int32) []byte {
	e.inst.mu.Lock()
	defer e.inst.mu.Unlock()
	v := e.inst.outbound[id]
	delete(e.inst.outbound, id)
	return v
}

// OpenSteering clears the steering queue and accepts steers for a turn.
func (e *Engine) OpenSteering() {
	e.inst.mu.Lock()
	defer e.inst.mu.Unlock()
	e.inst.clearSteeringLocked()
	e.inst.steeringOpen = true
}

// CloseSteering stops accepting steers and drops queued ones.
func (e *Engine) CloseSteering() { e.inst.closeSteering() }

// Steer queues text for the running turn (fx-sdk.js queueSteering).
func (e *Engine) Steer(id, text string) error {
	inst := e.inst
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if !inst.steeringOpen {
		return errors.New("no prompt is running")
	}
	value := []byte(text)
	switch {
	case len(value) == 0:
		return errors.New("steering text cannot be empty")
	case len(value) > maxSteeringMessageBytes:
		return fmt.Errorf("steering text exceeds the %d byte libfx limit", maxSteeringMessageBytes)
	case len(inst.steering) >= maxSteeringMessages || len(value) > maxSteeringQueueBytes-inst.steeringBytes:
		return errors.New("steering queue is full")
	}
	inst.steering = append(inst.steering, steer{id: id, value: value})
	inst.steeringBytes += len(value)
	return nil
}

// WithdrawSteering removes a queued steer the core has not taken yet.
func (e *Engine) WithdrawSteering(id string) bool {
	inst := e.inst
	inst.mu.Lock()
	defer inst.mu.Unlock()
	for i, s := range inst.steering {
		if s.id == id {
			inst.steeringBytes -= len(s.value)
			inst.steering = append(inst.steering[:i], inst.steering[i+1:]...)
			return true
		}
	}
	return false
}

// HandOffSteering ends the running turn at its next safe point.
func (e *Engine) HandOffSteering() bool {
	e.inst.mu.Lock()
	defer e.inst.mu.Unlock()
	if !e.inst.steeringOpen {
		return false
	}
	e.inst.steeringHandoff = true
	return true
}

// AbortHostEffects cancels in-flight HTTP streams, requests and tool calls.
func (e *Engine) AbortHostEffects() { e.inst.abortHostEffects() }

// Done is closed when the core has exited.
func (e *Engine) Done() <-chan struct{} { return e.done }

// ExitCode is the core's exit code once Done is closed.
func (e *Engine) ExitCode() uint32 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.exitCode
}

// Err is the abnormal-exit error once Done is closed, else nil.
func (e *Engine) Err() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.exitErr
}

// Close closes the core's stdin so it exits, waits up to 5 s (or ctx), then
// stops it. It waits for a blocked OnNotification handler only until ctx
// ends or another 5 s pass. It returns the abnormal-exit error, if any, from before Close.
func (e *Engine) Close(ctx context.Context) error {
	e.mu.Lock()
	alreadyExited := e.pendingDone
	priorErr := e.exitErr
	e.closing = true
	e.mu.Unlock()
	e.inst.closeSteering()
	e.inst.stdin.close()
	t := time.NewTimer(closeGrace)
	defer t.Stop()
	select {
	case <-e.done:
	case <-t.C:
	case <-ctx.Done():
	}
	e.runCancel()
	// Every blocking host call watches runCtx, so the guest stops promptly.
	<-e.done
	// A notification handler that blocks (OnNotification runs on the
	// dispatcher) must not hang Close: wait for the dispatcher only until
	// ctx ends or the grace period passes. It still fails pending requests
	// once the handler returns.
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(closeGrace)
	select {
	case <-e.dispatched:
	case <-t.C:
	case <-ctx.Done():
	}
	_ = e.mod.Close(context.Background())
	if alreadyExited {
		return priorErr
	}
	return nil
}

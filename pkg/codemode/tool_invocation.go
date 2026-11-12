package codemode

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/internal/third_party/qjs"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// toolBridge dispatches `tools.<name>(input)` calls made from sandboxed
// JavaScript to the corresponding host tool. One bridge is created per
// Run/RunCodeMode invocation. Mirrors TypeScript's invokeHostTool
// (code-mode/src/tool-invocation.ts) plus the per-invocation bookkeeping
// from run-code-mode.ts's createHostFunctions/invokeCodeModeTool, and (see
// the package doc's "Deterministic replay" section) the replay/resume
// machinery from run-code-mode.ts's context.resume handling.
type toolBridge struct {
	ctx           context.Context
	tools         ToolSet
	baseOptions   types.ToolExecutionOptions
	options       *Options
	policy        resolvedPolicy
	outerToolCall string

	// replayLedger holds every call from a prior invocation of the
	// identical source that had already completed, keyed by its 0-based
	// call index (see replayRecord's doc comment for why this is sparse,
	// not a "calls 0..N-1" prefix, since CM3's concurrent batching); start
	// replays an index present here with its recorded outcome instead of
	// calling the real host tool. nil on a fresh
	// (non-continuation) invocation.
	replayLedger map[int]replayRecord

	// resumePendings/resumeResolutions cover exactly the call indices of
	// the continuation's PendingInterruptions (recovered from each one's
	// RunInterruptionID via callIndexForPending, not from their position
	// in the batch -- see prepareContinuation in run_code_mode.go): each
	// is resumed with its resolution (skipping approval, for the approval
	// kind, or populating types.ToolExecutionOptions.CodeModeInterrupt,
	// for any other kind) instead of interrupting again. Parallel maps,
	// keyed by call index; nil/empty on a fresh invocation. A call index
	// present in neither replayLedger nor here executes for real, exactly
	// like a fresh invocation.
	resumePendings    map[int]PendingInterruption
	resumeResolutions map[int]interface{}

	mu           sync.Mutex
	requestCount int

	// committed accumulates every call settled so far this invocation,
	// keyed by its 0-based call index (see replayRecord's doc comment for
	// why this must be index-keyed rather than append-ordered: a
	// later-dispatched call can settle before an earlier-dispatched one
	// still pending approval in the same batch) -- short-circuited replay
	// calls and resumed/fresh real calls alike -- so a *new* interrupt
	// raised later in this same invocation can encode the next
	// continuation's replay ledger from it (see buildInterruptResult in
	// run_code_mode.go).
	committed map[int]replayRecord

	// pendingBatch accumulates every call that decided to interrupt
	// (approval under ApprovalModeInterrupt, or a host tool calling
	// RequestCodeModeInterrupt) during the current invocation, in the call
	// order of their request numbers. A call that decides to interrupt is
	// never settled (see finish): the sandboxed script keeps running --
	// starting further calls, e.g. the rest of a `Promise.all([...])`
	// array -- until the job queue is idle with no call in flight
	// (driveCodeModeExecution), at which point every entry collected here
	// so far is surfaced together as one multi-item Interrupt batch.
	// This is what lets Promise.all([tools.a(x), tools.b(y)]), where both
	// need approval, produce a single two-item Continuation instead of two
	// single-item ones chained together.
	pendingBatch []PendingInterruption

	// fatalErr, once set, wins over everything else once
	// driveCodeModeExecution next polls the bridge: an aborted context or an exceeded bridge-request
	// limit must stop the whole invocation immediately, not just the one
	// call that observed it (mirrors TypeScript's failTerminal, which
	// aborts the entire worker run for exactly these two conditions,
	// outside the per-call promise machinery entirely -- see
	// run-code-mode.ts's markWorkerRequest). Every other bridge failure
	// (an unknown tool, a failing tool.Execute, a denied callback-mode
	// approval, an oversized payload, ...) only rejects its own call's
	// Promise, exactly as TypeScript's invokeCodeModeTool throwing inside
	// one `__codeMode.toolN` async host function only rejects that one
	// call.
	fatalErr CodeModeError

	// typedErrs records every CodeModeError produced by a bridge call, so
	// RunCodeMode can re-surface the original typed error (via
	// preservedCodeModeErr) instead of the generic JavaScript exception
	// text that comes back once it round-trips through the sandbox
	// (mirrors TypeScript's codeModeErrors accumulator /
	// findPreservedCodeModeError).
	typedErrs []CodeModeError

	// The asynchronous half of the bridge (see start). toolCtx is the
	// context host tools run under: the caller's, cancelled once the
	// invocation ends (TS passes run's invocation abort signal).
	toolCtx     context.Context
	cancelTools context.CancelFunc
	toolNames   []string
	guest       *qjs.Value
	determinism runDeterminism

	// inFlight counts started host calls whose outcome the drive loop has
	// not taken yet (TS run's inFlightBridgeRequests); done holds the
	// outcomes the tool goroutines produced, in completion order, and
	// notify wakes the drive loop.
	inFlight int
	done     []bridgeCompletion
	notify   chan struct{}
	closed   bool
	wg       sync.WaitGroup

	// Responses reach the guest one at a time in settlement order (TS run's
	// queueBridgeResponse): nextSettlementOrder numbers each settled call,
	// starting after the replayed ledger's highest order, and
	// nextResponseOrder is the next order to deliver. immediate holds
	// replayed outcomes recorded without an order (continuations made
	// before settlement orders were recorded), delivered as they come.
	ready               map[int]bridgeResponse
	immediate           []bridgeResponse
	nextSettlementOrder int
	nextResponseOrder   int

	// finished is set once the script itself produced a result (fulfilled
	// or rejected), as opposed to stopping on an interruption or a fatal
	// error.
	finished bool

	// batchIndex is pendingBatch's call numbers, kept so the batch is
	// reported in call order (TS sorts the pending interruption indexes).
	batchIndex []int
}

// bridgeCompletion is one host call's outcome from its goroutine.
type bridgeCompletion struct {
	n         int
	toolName  string
	inputJSON string
	output    string
	err       error
}

// bridgeResponse is one settlement to deliver to the guest.
type bridgeResponse struct {
	id         int
	ok         bool
	outputJSON string
	errMessage string
	dateNowMs  int64
}

func newToolBridge(ctx context.Context, input RunInput, options *Options, policy resolvedPolicy, outerToolCall string, replayLedger map[int]replayRecord, resumePendings map[int]PendingInterruption, resumeResolutions map[int]interface{}) *toolBridge {
	base := types.ToolExecutionOptions{}
	if input.ToolExecutionOptions != nil {
		base = *input.ToolExecutionOptions
	}
	toolNames := make([]string, 0, len(input.Tools))
	for name := range input.Tools {
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)
	toolCtx, cancelTools := context.WithCancel(ctx)
	nextSettlementOrder := 0
	for _, rec := range replayLedger {
		nextSettlementOrder = max(nextSettlementOrder, rec.SettledOrder)
	}
	return &toolBridge{
		ctx:                 ctx,
		toolCtx:             toolCtx,
		cancelTools:         cancelTools,
		toolNames:           toolNames,
		determinism:         newRunDeterminism(),
		notify:              make(chan struct{}, 1),
		ready:               make(map[int]bridgeResponse),
		nextSettlementOrder: nextSettlementOrder,
		nextResponseOrder:   1,
		tools:               input.Tools,
		baseOptions:         base,
		options:             options,
		policy:              policy,
		outerToolCall:       outerToolCall,
		replayLedger:        replayLedger,
		resumePendings:      resumePendings,
		resumeResolutions:   resumeResolutions,
		committed:           make(map[int]replayRecord),
	}
}

// start begins one host call the guest has observed (see
// codeModeGuestSource) and returns its request number, which the guest uses
// to match the settlement. It runs on the sandbox goroutine, in the order
// the guest starts calls, so request numbers (and the `:tool-N` tool call
// IDs) follow TS run's request order. Mirrors run's
// handleHostFunctionRequest: an aborted invocation, the bridge request limit
// and the in-flight limit are terminal (the call never settles; the drive
// loop stops on the fatal error); a call already settled in the
// continuation's ledger is replayed with its recorded outcome, in its
// recorded settlement order; anything else runs on its own goroutine, so
// several host calls are in flight together as in TS.
//
// serializeErr is the error serializing the call's input failed with (TS
// rejects the call with the JSON payload error); the call still counts as a
// request.
func (b *toolBridge) start(toolName, inputJSON string, serializeErr error) int {
	if cErr := b.ctx.Err(); cErr != nil {
		b.setFatal(NewAbortedError())
		return 0
	}

	b.mu.Lock()
	b.requestCount++
	n := b.requestCount
	inFlight := b.inFlight
	b.mu.Unlock()
	if b.policy.MaxBridgeRequests > 0 && n > b.policy.MaxBridgeRequests {
		b.setFatal(NewBridgeLimitError(
			fmt.Sprintf("JavaScript runtime exceeded the %d bridge request limit.", b.policy.MaxBridgeRequests),
			map[string]interface{}{"maxBridgeRequests": b.policy.MaxBridgeRequests},
		))
		return n
	}
	if b.policy.MaxInFlightBridgeRequests > 0 && inFlight >= b.policy.MaxInFlightBridgeRequests {
		b.setFatal(NewBridgeLimitError(
			fmt.Sprintf("JavaScript runtime exceeded the %d in-flight bridge request limit.", b.policy.MaxInFlightBridgeRequests),
			map[string]interface{}{"maxInFlightBridgeRequests": b.policy.MaxInFlightBridgeRequests},
		))
		return n
	}
	idx := n - 1 // 0-based call index, aligned with replayLedger/resumePendings.

	// Replay a call already settled by a prior invocation of this same
	// source: deliver its recorded outcome without touching the real host
	// tool, so side effects never repeat. Keyed by call index, not position
	// (see replayLedger's doc comment). TS run's assertReplayMatches first
	// checks the call is the recorded one.
	if rec, ok := b.replayLedger[idx]; ok {
		if rec.ToolName != toolName || rec.InputJSON != inputJSON {
			b.setFatal(b.replayDivergedError(n, rec.ToolName, toolName))
			return n
		}
		b.mu.Lock()
		b.committed[idx] = rec
		resp := bridgeResponse{id: n, ok: rec.Status != replayStatusRejected, outputJSON: rec.OutputJSON, errMessage: rec.ErrorMessage, dateNowMs: rec.DateNowMs}
		var qerr CodeModeError
		if rec.SettledOrder == 0 {
			b.immediate = append(b.immediate, resp)
		} else {
			qerr = b.queueResponseLocked(rec.SettledOrder, resp)
		}
		b.mu.Unlock()
		if qerr != nil {
			b.setFatal(qerr)
		}
		return n
	}
	if pending, ok := b.resumePendings[idx]; ok && !sameRecordedCall(pending, toolName, inputJSON) {
		b.setFatal(b.replayDivergedError(n, pending.ToolName, toolName))
		return n
	}

	if serializeErr != nil {
		b.finish(bridgeCompletion{n: n, toolName: toolName, inputJSON: inputJSON, err: serializeErr}, false)
		return n
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return n
	}
	b.inFlight++
	b.wg.Add(1)
	b.mu.Unlock()
	go func() {
		defer b.wg.Done()
		out, err := b.invoke(n, toolName, inputJSON)
		b.mu.Lock()
		b.done = append(b.done, bridgeCompletion{n: n, toolName: toolName, inputJSON: inputJSON, output: out, err: err})
		b.mu.Unlock()
		select {
		case b.notify <- struct{}{}:
		default:
		}
	}()
	return n
}

// replayDivergedError is TS run's assertReplayMatches error.
func (b *toolBridge) replayDivergedError(n int, expected, received string) CodeModeError {
	return NewProtocolError("Continuation replay diverged from the recorded host function ledger.", map[string]interface{}{
		"bridgeIndex":              n,
		"expectedHostFunctionName": b.hostFunctionName(expected),
		"receivedHostFunctionName": b.hostFunctionName(received),
	})
}

// hostFunctionName is the TS code-mode host function a tool name is bound
// to: `__codeMode.tool<index>` for a known tool, `__codeMode.missing`
// otherwise.
func (b *toolBridge) hostFunctionName(toolName string) string {
	for i, name := range b.toolNames {
		if name == toolName {
			return "__codeMode.tool" + strconv.Itoa(i)
		}
	}
	return "__codeMode.missing"
}

// sameRecordedCall reports whether a resumed call is the one the pending
// interruption recorded (TS assertReplayMatches compares the host function
// and its serialized arguments).
func sameRecordedCall(pending PendingInterruption, toolName, inputJSON string) bool {
	if pending.ToolName != toolName {
		return false
	}
	var got interface{}
	if inputJSON != "" {
		if err := json.Unmarshal([]byte(inputJSON), &got); err != nil {
			return false
		}
	}
	want := pending.Input
	if want != nil {
		// The recorded input went through a JSON round trip already when the
		// continuation was encoded; normalize Go-side values the same way.
		if data, err := json.Marshal(want); err == nil {
			want = nil
			if uerr := json.Unmarshal(data, &want); uerr != nil {
				return false
			}
		}
	}
	return reflect.DeepEqual(got, want)
}

// queueResponseLocked queues resp for delivery at settlement order order
// (TS run's queueBridgeResponse). b.mu must be held.
func (b *toolBridge) queueResponseLocked(order int, resp bridgeResponse) CodeModeError {
	if _, dup := b.ready[order]; dup || order < b.nextResponseOrder {
		return NewProtocolError("Duplicate bridge settlement order.", map[string]interface{}{"settledOrder": order})
	}
	b.ready[order] = resp
	return nil
}

// finish records one host call's outcome on the sandbox goroutine (TS run's
// handleHostFunctionRequest tail / handleHostFunctionFailure): a value or an
// ordinary failure is numbered in settlement order, recorded in the ledger
// and queued for the guest; an interruption joins the pending batch and
// never settles; an abort or a bridge limit is terminal. wasInFlight is
// whether the call counted as in flight.
func (b *toolBridge) finish(c bridgeCompletion, wasInFlight bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if wasInFlight {
		b.inFlight--
	}
	idx := c.n - 1
	if c.err == nil {
		b.nextSettlementOrder++
		order := b.nextSettlementOrder
		now := time.Now().UnixMilli()
		b.committed[idx] = replayRecord{ToolName: c.toolName, InputJSON: c.inputJSON, OutputJSON: c.output, SettledOrder: order, DateNowMs: now}
		if qerr := b.queueResponseLocked(order, bridgeResponse{id: c.n, ok: true, outputJSON: c.output, dateNowMs: now}); qerr != nil && b.fatalErr == nil {
			b.fatalErr = qerr
		}
		return
	}
	switch e := c.err.(type) {
	case *interruptUnwind:
		// Keep the batch in call order.
		pos := sort.SearchInts(b.batchIndex, c.n)
		b.batchIndex = append(b.batchIndex, 0)
		copy(b.batchIndex[pos+1:], b.batchIndex[pos:])
		b.batchIndex[pos] = c.n
		b.pendingBatch = append(b.pendingBatch, PendingInterruption{})
		copy(b.pendingBatch[pos+1:], b.pendingBatch[pos:])
		b.pendingBatch[pos] = e.pending
		return
	case *AbortedError, *BridgeLimitError:
		if b.fatalErr == nil {
			b.fatalErr = c.err.(CodeModeError)
		}
		return
	}
	b.nextSettlementOrder++
	order := b.nextSettlementOrder
	message := c.err.Error()
	now := time.Now().UnixMilli()
	b.committed[idx] = replayRecord{ToolName: c.toolName, InputJSON: c.inputJSON, Status: replayStatusRejected, ErrorMessage: message, SettledOrder: order, DateNowMs: now}
	if qerr := b.queueResponseLocked(order, bridgeResponse{id: c.n, errMessage: message, dateNowMs: now}); qerr != nil && b.fatalErr == nil {
		b.fatalErr = qerr
	}
}

// takeCompletion moves one finished host call's outcome into the bridge
// state, reporting whether there was one.
func (b *toolBridge) takeCompletion() bool {
	b.mu.Lock()
	if len(b.done) == 0 {
		b.mu.Unlock()
		return false
	}
	c := b.done[0]
	b.done = b.done[1:]
	b.mu.Unlock()
	b.finish(c, true)
	return true
}

// nextResponse returns the next settlement to deliver, if one is ready.
func (b *toolBridge) nextResponse() (bridgeResponse, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.immediate) > 0 {
		resp := b.immediate[0]
		b.immediate = b.immediate[1:]
		return resp, true
	}
	resp, ok := b.ready[b.nextResponseOrder]
	if !ok {
		return bridgeResponse{}, false
	}
	delete(b.ready, b.nextResponseOrder)
	b.nextResponseOrder++
	return resp, true
}

// inFlightCount returns how many started host calls have not finished.
func (b *toolBridge) inFlightCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inFlight
}

// waitCompletion blocks until a host call finishes or ctx is done.
func (b *toolBridge) waitCompletion(ctx context.Context) error {
	for {
		b.mu.Lock()
		ready := len(b.done) > 0
		b.mu.Unlock()
		if ready {
			return nil
		}
		select {
		case <-b.notify:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// shutdown ends the invocation's host calls: their context is cancelled (TS
// aborts the invocation signal) and RunCodeMode waits for their goroutines,
// which return promptly once cancelled (raceAgainstAbort).
func (b *toolBridge) shutdown() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.cancelTools()
	b.wg.Wait()
}

// markFinished records that the script produced a result.
func (b *toolBridge) markFinished() {
	b.mu.Lock()
	b.finished = true
	b.mu.Unlock()
}

// finishedScript reports whether the script produced a result.
func (b *toolBridge) finishedScript() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.finished
}

// ledgerLength is TS run's continuation ledger length: one past the highest
// call index the continuation recorded (settled or pending).
func (b *toolBridge) ledgerLength() int {
	length := 0
	for idx := range b.replayLedger {
		length = max(length, idx+1)
	}
	for idx := range b.resumePendings {
		length = max(length, idx+1)
	}
	return length
}

// invoke runs one host call on its own goroutine (see start): input size and
// validation, approval, the resumed interruption's resolution, and the tool
// itself. n is the call's 1-based request number. It touches no bridge state
// other than typedErrs; finish records the outcome.
func (b *toolBridge) invoke(n int, toolName, inputJSON string) (outputJSON string, err error) {
	defer func() {
		if err != nil {
			if cmErr, ok := err.(CodeModeError); ok {
				b.mu.Lock()
				b.typedErrs = append(b.typedErrs, cmErr)
				b.mu.Unlock()
			}
		}
	}()

	if cErr := b.toolCtx.Err(); cErr != nil {
		return "", NewAbortedError()
	}
	idx := n - 1

	// TS invokeCodeModeTool serializes the input against maxToolInputBytes
	// before invokeHostTool looks the tool up, so an oversized input fails
	// with the size error even for an unknown tool.
	if err := assertJSONPayloadSize(inputJSON, b.policy.MaxToolInputBytes, "Tool \""+toolName+"\" input"); err != nil {
		return "", err
	}

	tool, ok := b.tools[toolName]
	if !ok {
		names := make([]string, 0, len(b.tools))
		for name := range b.tools {
			names = append(names, name)
		}
		// TS lists Object.keys(tools) (insertion order); a Go map has none,
		// so the list is sorted to stay deterministic.
		sort.Strings(names)
		return "", NewToolError(fmt.Sprintf("Unknown tool: %s", toolName), map[string]interface{}{
			"toolName":       toolName,
			"availableTools": names,
		})
	}
	if tool.Execute == nil {
		return "", NewToolError("Tool \""+toolName+"\" does not have execute().", map[string]interface{}{"toolName": toolName})
	}

	inputVal, ferr := fromJSONPayload(inputJSON)
	if ferr != nil {
		return "", ferr
	}
	input, isObject := inputVal.(map[string]interface{})
	if tool.ValidateInput != nil {
		// TS validateToolInput uses asSchema(inputSchema).validate: for
		// jsonSchema(schema, {validate}) the tool's own validator replaces
		// the schema check, and its value (validation.value) is the input
		// approval and execute see (as pkg/ai's tool call pipeline).
		raw := inputVal
		if !isObject && inputJSON == "" {
			raw = map[string]interface{}{}
		}
		validated, verr := tool.ValidateInput(raw)
		if verr != nil {
			return "", NewToolError(
				"Invalid input for tool \""+toolName+"\": "+verr.Error(),
				map[string]interface{}{"toolName": toolName, "input": inputVal, "cause": verr.Error()},
			)
		}
		if m, ok := validated.(map[string]interface{}); ok {
			input, isObject = m, true
		}
		if !isObject && inputJSON != "" {
			// A Go tool's Execute takes an object, so a non-object argument
			// the validator accepted is still an input error.
			cause := fmt.Sprintf("expected an object, received %s", jsonKindName(inputVal))
			return "", NewToolError(
				"Invalid input for tool \""+toolName+"\": "+cause,
				map[string]interface{}{"toolName": toolName, "input": inputVal, "cause": cause},
			)
		}
	} else if !isObject && inputJSON != "" {
		// TS passes the sandbox value through unchanged, so a non-object
		// argument (tools.x("text"), tools.x([1]), tools.x(null)) fails input
		// validation instead of being replaced by an empty object. A Go tool's
		// Execute takes an object, so such a value is always an input error.
		cause := fmt.Sprintf("expected an object, received %s", jsonKindName(inputVal))
		if validator := toolInputValidator(tool); validator != nil {
			if verr := validator.Validate(inputVal); verr != nil {
				cause = verr.Error()
			}
		}
		return "", NewToolError(
			"Invalid input for tool \""+toolName+"\": "+cause,
			map[string]interface{}{"toolName": toolName, "input": inputVal, "cause": cause},
		)
	}
	if input == nil {
		input = map[string]interface{}{}
	}

	if validator := toolInputValidator(tool); validator != nil && tool.ValidateInput == nil {
		// TS's validateToolInput uses asSchema(...).validate, which for a
		// zod schema fills .default() values as part of validation and
		// passes validation.value (not the raw input) on to execute. Apply
		// defaults before validating for the same effect (schema.Validate
		// alone does not fill defaults).
		// The error details carry the input as the sandbox passed it (TS
		// reports `input`, not validation.value).
		rawInput := inputVal
		input = applyToolInputDefaults(input, validator)
		if verr := validator.Validate(input); verr != nil {
			return "", NewToolError(
				"Invalid input for tool \""+toolName+"\": "+verr.Error(),
				map[string]interface{}{"toolName": toolName, "input": rawInput, "cause": verr.Error()},
			)
		}
	}

	toolCallID := fmt.Sprintf("%s:tool-%d", b.outerToolCall, n)
	execOptions := b.baseOptions
	execOptions.ToolCallID = toolCallID

	// Determine whether this call is resuming a previously pending
	// interruption (see the replayLedger/resumePendings doc above), keyed
	// by call index directly.
	skipApproval := false
	if pending, ok := b.resumePendings[idx]; ok {
		resolution := b.resumeResolutions[idx]
		if pending.Payload.Kind() == ToolApprovalKind {
			decision, derr := normalizeApprovalResolution(resolution)
			if derr != nil {
				return "", derr
			}
			if !decision.Approved {
				return "", NewToolApprovalDeniedError(pending.ToolName, pending.Input, pending.ToolCallID, decision.Reason)
			}
			skipApproval = true
		} else {
			execOptions.CodeModeInterrupt = &InterruptExecutionContext{
				InterruptID: pending.InterruptID,
				Payload:     pending.Payload,
				Resolution:  resolution,
			}
		}
	}

	if !skipApproval {
		needsApproval, aerr := raceAgainstAbort(b.toolCtx, func() (bool, error) {
			return resolveNeedsApproval(b.toolCtx, tool, toolName, input, execOptions)
		})
		if aerr != nil {
			return "", aerr
		}
		if needsApproval {
			if err := b.resolveApproval(tool, toolName, input, inputJSON, toolCallID, n); err != nil {
				return "", err
			}
		}
	}

	output, oerr := raceAgainstAbort(b.toolCtx, func() (interface{}, error) {
		return tool.Execute(b.toolCtx, input, execOptions)
	})
	if oerr != nil {
		if _, ok := oerr.(CodeModeError); ok {
			// Mirrors TypeScript's invokeCodeModeTool: a CodeModeError
			// (cancellation winning raceAgainstAbort, or one the tool
			// itself returned, such as RequestCodeModeInterrupt's
			// *ProtocolError for a malformed payload) propagates as-is,
			// unsanitized, and is preserved for the caller.
			return "", oerr
		}
		if sig, ok := oerr.(*interruptSignal); ok {
			// The tool called RequestCodeModeInterrupt: pause here
			// instead of sanitizing this as a tool failure. The pending
			// interruption keeps the sandbox's raw argument, as in TS.
			return "", b.raiseInterrupt(toolName, rawToolInput(inputJSON), toolCallID, n, sig.payload)
		}
		// Mirrors TypeScript's invokeCodeModeTool: any non-CodeModeError
		// thrown by a host tool is sanitized to a generic message, with TS's
		// CODE_MODE_HOST_TOOL_ERROR code, so raw tool internals never leak
		// into the sandbox. The cause stays in Details for the Go caller.
		return "", newHostToolFailedError(toolName, oerr)
	}

	outJSON, jerr := toJSONPayload(output, b.policy.MaxToolOutputBytes, "Tool \""+toolName+"\" output")
	if jerr != nil {
		return "", jerr
	}
	return outJSON, nil
}

// raiseInterrupt builds the pending interruption for the call numbered n
// (1-based, matching invoke's toolCallID numbering) and returns the
// sentinel error finish recognizes: the call joins the invocation's pending
// batch (see pendingBatch's doc comment) and its Promise is left
// unresolved, so the sandboxed script keeps making progress (e.g. the rest
// of a concurrent Promise.all array) until nothing more can run. An
// interrupted call stops counting as in flight once finish takes it, as in
// TS run.
func (b *toolBridge) raiseInterrupt(toolName string, input interface{}, toolCallID string, n int, payload InterruptPayload) error {
	pending := PendingInterruption{
		RunInterruptionID: fmt.Sprintf("interrupt-%d", n),
		InterruptID:       toolCallID + ":interrupt",
		ToolName:          toolName,
		ToolCallID:        toolCallID,
		Input:             input,
		Payload:           payload,
	}
	return &interruptUnwind{pending: pending}
}

// rawToolInput decodes a call's argument as the sandbox passed it (nil when
// it passed none), freshly, so neither schema defaults nor a tool that
// modified its input map change the recorded interruption input. inputJSON
// was already decoded once by invoke, so it cannot fail here.
func rawToolInput(inputJSON string) interface{} {
	v, _ := fromJSONPayload(inputJSON)
	return v
}

// interruptUnwind is the sentinel error returned by invoke when a call
// raises a pending interruption. finish recognizes it by type and leaves
// the call's Promise deliberately unresolved instead of rejecting it; a
// script that returns while such a call is still pending fails with
// *DetachedBridgeRequestError (guestDetachedCheck).
type interruptUnwind struct {
	pending PendingInterruption
}

func (e *interruptUnwind) Error() string {
	return fmt.Sprintf("Code mode execution paused: tool %q requested an interruption (kind=%q).", e.pending.ToolName, e.pending.Payload.Kind())
}

// batchSnapshot returns a copy of the interruptions collected so far (see
// pendingBatch's doc comment).
func (b *toolBridge) batchSnapshot() []PendingInterruption {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.pendingBatch) == 0 {
		return nil
	}
	return append([]PendingInterruption(nil), b.pendingBatch...)
}

// committedSnapshot returns a copy of every call resolved so far, keyed by
// call index (see the committed field's doc comment).
func (b *toolBridge) committedSnapshot() map[int]replayRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	snap := make(map[int]replayRecord, len(b.committed))
	for idx, rec := range b.committed {
		snap[idx] = rec
	}
	return snap
}

// preservedCodeModeErr returns the typed CodeModeError a bridge call
// produced whose rejection is the error the sandboxed script failed with
// (message is that error's `message`), if any (see typedErrs' doc comment).
// TS matches the run's error code against codeModeErrors; the Go sandbox
// sees the rejection value itself, so it matches its message. A script that
// caught a tool error and then failed some other way (its own throw, a
// timeout) is not reported as that tool error.
func (b *toolBridge) preservedCodeModeErr(message string) CodeModeError {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.typedErrs) - 1; i >= 0; i-- {
		if b.typedErrs[i].Error() == message {
			return b.typedErrs[i]
		}
	}
	return nil
}

// requestTotal returns how many host calls this invocation has made (TS
// run's totalBridgeRequests).
func (b *toolBridge) requestTotal() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.requestCount
}

// fatal returns the invocation's fatal error, if any (see fatalErr's doc
// comment).
func (b *toolBridge) fatal() CodeModeError {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.fatalErr
}

// setFatal records err as the invocation's fatal error unless one is
// already recorded (first one wins, matching TypeScript's failTerminal
// being a no-op once terminalReached is set).
func (b *toolBridge) setFatal(err CodeModeError) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fatalErr == nil {
		b.fatalErr = err
	}
}

// inputJSON is the call's argument as the sandbox passed it: TS records an
// interruption's input from the host function arguments
// (toPendingInterruptions), not from the validated value.
func (b *toolBridge) resolveApproval(tool types.Tool, toolName string, input map[string]interface{}, inputJSON string, toolCallID string, n int) error {
	mode := ApprovalModeCallback
	var onApprovalRequired OnApprovalRequiredFunc
	if b.options != nil && b.options.Approval != nil {
		if b.options.Approval.Mode != "" {
			mode = b.options.Approval.Mode
		}
		onApprovalRequired = b.options.Approval.OnApprovalRequired
	}

	if mode == ApprovalModeInterrupt {
		return b.raiseInterrupt(toolName, rawToolInput(inputJSON), toolCallID, n, InterruptPayload{"kind": ToolApprovalKind})
	}

	if onApprovalRequired == nil {
		return NewToolApprovalRequiredError(toolName, input, toolCallID)
	}

	decision, derr := raceAgainstAbort(b.toolCtx, func() (ApprovalDecision, error) {
		return onApprovalRequired(b.toolCtx, ApprovalRequest{ToolName: toolName, Input: input, ToolCallID: toolCallID})
	})
	if derr != nil {
		return derr
	}
	if !decision.Approved {
		return NewToolApprovalDeniedError(toolName, input, toolCallID, decision.Reason)
	}
	return nil
}

// resolveNeedsApproval evaluates a host tool's approval requirement.
// Mirrors TypeScript's requiresApproval, which honors tool.needsApproval as a
// boolean or any function. The Go Tool also carries the outer SDK's approval
// values, read as pkg/ai's resolveToolApprovalValue reads a tool's own
// setting: an unnamed function literal with an approval function signature
// is that function, ToolApprovalStatusUserApproval (or "user-approval")
// needs approval, ToolApprovalStatusDenied (or "denied") fails the call with
// *ToolApprovalDeniedError, and approved / not-applicable / "" need none.
// Any other value fails closed with *ToolError instead of running the tool
// unapproved.
func resolveNeedsApproval(ctx context.Context, tool types.Tool, toolName string, input map[string]interface{}, options types.ToolExecutionOptions) (bool, error) {
	setting := tool.ToolApproval
	if setting == nil {
		setting = tool.NeedsApproval
	}
	switch fn := setting.(type) {
	case func(context.Context, map[string]interface{}, types.ToolNeedsApprovalOptions) bool:
		setting = types.ToolNeedsApprovalFunc(fn)
	case func(context.Context, map[string]interface{}) bool:
		setting = types.NeedsApprovalFunc(fn) //nolint:staticcheck // legacy function type still accepted for backward compatibility
	}
	switch v := setting.(type) {
	case nil:
		return false, nil
	case bool:
		return v, nil
	case types.NeedsApprovalFunc: //nolint:staticcheck // legacy function type still accepted for backward compatibility
		if v == nil {
			return false, nil
		}
		return v(ctx, input), nil
	case types.ToolNeedsApprovalFunc:
		if v == nil {
			return false, nil
		}
		return v(ctx, input, types.ToolNeedsApprovalOptions{
			ToolCallID: options.ToolCallID,
			Messages:   options.Messages,
			Context:    options.ToolContext,
		}), nil
	case types.ToolApprovalStatus:
		return approvalStatusNeedsApproval(v, toolName, input, options.ToolCallID)
	case string:
		return approvalStatusNeedsApproval(types.ToolApprovalStatus(v), toolName, input, options.ToolCallID)
	}
	return false, NewToolError(
		fmt.Sprintf("Tool %q has an unsupported approval setting of type %T: use a bool, a ToolApprovalStatus or a ToolNeedsApprovalFunc.", toolName, setting),
		map[string]interface{}{"toolName": toolName},
	)
}

// approvalStatusNeedsApproval maps a static approval status on a host tool:
// user-approval asks, denied rejects the call, approved / not-applicable /
// "" run it, and an unknown status fails closed.
func approvalStatusNeedsApproval(status types.ToolApprovalStatus, toolName string, input map[string]interface{}, toolCallID string) (bool, error) {
	switch status {
	case types.ToolApprovalStatusUserApproval:
		return true, nil
	case types.ToolApprovalStatusDenied:
		return false, NewToolApprovalDeniedError(toolName, input, toolCallID, "")
	case "", types.ToolApprovalStatusNotApplicable, types.ToolApprovalStatusApproved:
		return false, nil
	}
	return false, NewToolError(
		fmt.Sprintf("Tool %q has an unknown approval status %q.", toolName, status),
		map[string]interface{}{"toolName": toolName},
	)
}

// applyToolInputDefaults fills any JSON-Schema "default" values missing from
// input. Mirrors pkg/ai/tool_call_pipeline.go's applyToolCallInputDefaults;
// never mutates input, and returns it unchanged if the defaulted result
// can't be represented as an object.
func applyToolInputDefaults(input map[string]interface{}, validator schema.Validator) map[string]interface{} {
	defaulted := schema.ApplyDefaults(input, schema.NewSimpleJSONSchema(validator.JSONSchema()))
	if obj, ok := defaulted.(map[string]interface{}); ok {
		return obj
	}
	return input
}

// toolInputValidator returns a JSON-schema validator for the tool's input,
// or nil when the tool has no JSON schema. Mirrors
// pkg/ai/tool_call_pipeline.go's toolCallInputValidator.
func toolInputValidator(tool types.Tool) schema.Validator {
	if isHintedParameters(tool.Parameters) {
		if m, ok := schema.ParametersMap(tool.Parameters); ok && len(m) > 0 {
			return schema.NewJSONSchema(m)
		}
		return nil
	}
	switch s := tool.Parameters.(type) {
	case map[string]interface{}:
		if len(s) == 0 {
			return nil
		}
		return schema.NewJSONSchema(s)
	case schema.Schema:
		if isNilSchemaValue(s) {
			return nil
		}
		if v := s.Validator(); !isNilSchemaValue(v) {
			return v
		}
		return nil
	case schema.Validator:
		// A bare validator as Parameters (accepted by pkg/ai too): TS code
		// mode validates with asSchema(tool.inputSchema) for every schema.
		if isNilSchemaValue(s) {
			return nil
		}
		return s
	}
	return nil
}

// isNilSchemaValue reports whether v is nil or holds a nil pointer (a typed
// nil *schema.SimpleStructSchema panics in Validator()).
func isNilSchemaValue(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface:
		return rv.IsNil()
	}
	return false
}

// raceAgainstAbort runs fn in its own goroutine and returns its result,
// unless ctx is done first -- in which case it returns immediately with
// *AbortedError without waiting for fn to finish. If fn never returns (a
// misbehaving host tool that ignores context cancellation), its goroutine
// is abandoned; this is the same accepted trade-off runInSandbox makes for
// a timed-out sandbox invocation (see engine.go's doc comment) -- Go has
// no way to forcibly stop a goroutine, so the alternative is to hang
// indefinitely, which is worse.
//
// Mirrors TypeScript's raceAgainstAbort (code-mode/src/tool-invocation.ts),
// which races every nested host-tool step (needsApproval, the approval
// callback, execute) against the outer AbortSignal so cancellation takes
// effect immediately instead of only being observed between bridge calls.
func raceAgainstAbort[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, NewAbortedError()
	}

	type outcome struct {
		v   T
		err error
	}
	ch := make(chan outcome, 1)
	go func() {
		// A panic in a host tool must not take the process down (this runs on
		// its own goroutine, where nothing above can recover it): report it
		// as an ordinary tool failure, like a thrown error in TS.
		defer func() {
			if r := recover(); r != nil {
				ch <- outcome{zero, fmt.Errorf("host tool panicked: %v", r)}
			}
		}()
		v, err := fn()
		ch <- outcome{v, err}
	}()

	select {
	case out := <-ch:
		return out.v, out.err
	case <-ctx.Done():
		return zero, NewAbortedError()
	}
}

// isHintedParameters reports whether a tool's Parameters is a
// jsonorder.Hinted, in its value or pointer form.
func isHintedParameters(p interface{}) bool {
	switch p.(type) {
	case jsonorder.Hinted, *jsonorder.Hinted:
		return true
	}
	return false
}

func jsonKindName(v interface{}) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		return "number"
	}
}

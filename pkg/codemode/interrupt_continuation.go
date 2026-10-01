package codemode

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"strconv"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// replayRecord is one completed host tool call's recorded (tool name,
// input, output). Continuation.Token is the JSON encoding of a
// map[int]replayRecord (see encodeReplayLedger), keyed by each call's
// 0-based dispatch index: every call that had completed by the time this
// continuation's interruption batch was collected. Since CM3 (concurrent
// approval batching), this is sparse rather than a call-order prefix: a
// later-dispatched call can complete -- and be recorded -- before an
// earlier-dispatched one that is still pending approval in the same batch
// (e.g. `Promise.all([tools.guarded(x), tools.free(y)])`, where "free"
// needs no approval), so the ledger cannot be a plain "calls
// 0..len(ledger)-1" list; see the package doc's "Deterministic replay"
// section. Resuming decodes it back and short-circuits exactly the call
// indices present in it with their recorded OutputJSON instead of
// re-invoking the real host tool, so side effects never repeat -- an index
// absent from the ledger executes for real (or applies resume semantics;
// see resumePendings/resumeResolutions in toolBridge) regardless of
// whether it is less than the highest recorded index.
type replayRecord struct {
	ToolName   string `json:"toolName"`
	InputJSON  string `json:"inputJson"`
	OutputJSON string `json:"outputJson"`
}

func encodeReplayLedger(records map[int]replayRecord) (string, error) {
	wire := make(map[string]replayRecord, len(records))
	for idx, rec := range records {
		wire[strconv.Itoa(idx)] = rec
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return "", NewProtocolError("Failed to encode the code mode replay ledger.", nil)
	}
	return string(data), nil
}

func decodeReplayLedger(token string) (map[int]replayRecord, error) {
	var wire map[string]replayRecord
	if err := json.Unmarshal([]byte(token), &wire); err != nil {
		return nil, NewProtocolError("Code mode continuation token could not be decoded by this implementation.", nil)
	}
	records := make(map[int]replayRecord, len(wire))
	for key, rec := range wire {
		idx, err := strconv.Atoi(key)
		if err != nil || idx < 0 {
			return nil, NewProtocolError("Code mode continuation token could not be decoded by this implementation.", nil)
		}
		records[idx] = rec
	}
	return records, nil
}

// runInterruptionIndexPattern matches the RunInterruptionID format
// toolBridge.raiseInterrupt generates ("interrupt-%d", 1-based call
// number), mirroring TypeScript's interruptionIndex
// (code-mode/src/run-code-mode.ts: /^interrupt-(\d+)$/).
var runInterruptionIndexPattern = regexp.MustCompile(`^interrupt-(\d+)$`)

// callIndexForPending returns pending's 0-based dispatch call index,
// parsed from its RunInterruptionID. Every PendingInterruption this
// package produces has one in exactly this shape; a malformed one (e.g.
// from a hand-built or foreign continuation) is a protocol error.
func callIndexForPending(pending PendingInterruption) (int, error) {
	match := runInterruptionIndexPattern.FindStringSubmatch(pending.RunInterruptionID)
	if match == nil {
		return 0, NewProtocolError(
			"Code mode continuation has a malformed pending interruption id.",
			map[string]interface{}{"runInterruptionId": pending.RunInterruptionID},
		)
	}
	n, err := strconv.Atoi(match[1])
	if err != nil || n < 1 {
		return 0, NewProtocolError(
			"Code mode continuation has a malformed pending interruption id.",
			map[string]interface{}{"runInterruptionId": pending.RunInterruptionID},
		)
	}
	return n - 1, nil
}

// IsCodeModeInterrupt reports whether value is a valid, signed Interrupt:
// structurally well-formed, its embedded Continuation verifies (see
// VerifyCodeModeContinuation via security), and it matches the pending
// interruption at Continuation.Resolutions' length. Accepts *Interrupt,
// Interrupt, or a map[string]interface{} of the same shape (e.g. one
// decoded from JSON). Mirrors TypeScript's experimental_isCodeModeInterrupt.
func IsCodeModeInterrupt(value interface{}, security ...ContinuationSecurityOptions) bool {
	_, ok := asInterrupt(value, resolveSecurityArg(security))
	return ok
}

func resolveSecurityArg(security []ContinuationSecurityOptions) ContinuationSecurityOptions {
	if len(security) == 0 {
		return ContinuationSecurityOptions{}
	}
	return security[0]
}

// asInterrupt normalizes value to an *Interrupt if it is one (or decodes
// to one) and it passes assertInterruptMatchesLedger + continuation
// verification.
func asInterrupt(value interface{}, security ContinuationSecurityOptions) (*Interrupt, bool) {
	interrupt, ok := toInterrupt(value)
	if !ok {
		return nil, false
	}
	if interrupt.Type != InterruptTypeValue || interrupt.InterruptID == "" ||
		interrupt.ToolCallID == "" || interrupt.ToolName == "" ||
		interrupt.OuterToolCallID == "" || interrupt.Payload.Kind() == "" {
		return nil, false
	}
	if verifyContinuation(interrupt.Continuation, security) != nil {
		return nil, false
	}
	if assertInterruptMatchesLedger(*interrupt) != nil {
		return nil, false
	}
	return interrupt, true
}

// toInterrupt converts value to an *Interrupt without validating it,
// accepting the same shapes as asInterrupt.
func toInterrupt(value interface{}) (*Interrupt, bool) {
	switch v := value.(type) {
	case nil:
		return nil, false
	case *Interrupt:
		if v == nil {
			return nil, false
		}
		return v, true
	case Interrupt:
		return &v, true
	case map[string]interface{}:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		var interrupt Interrupt
		if err := json.Unmarshal(data, &interrupt); err != nil {
			return nil, false
		}
		return &interrupt, true
	default:
		return nil, false
	}
}

// assertInterruptMatchesLedger mirrors TypeScript's
// assertInterruptMatchesLedger: interrupt's own fields must match the
// pending interruption recorded at
// interrupt.Continuation.PendingInterruptions[len(interrupt.Continuation.Resolutions)]
// in its signed continuation, so a forged or stale Interrupt is rejected
// even if its embedded Continuation's signature is otherwise valid.
func assertInterruptMatchesLedger(interrupt Interrupt) error {
	if interrupt.Continuation.OuterToolCallID != interrupt.OuterToolCallID {
		return NewProtocolError("Code-mode interrupt outer tool call id does not match its continuation.", nil)
	}
	idx := len(interrupt.Continuation.Resolutions)
	if idx < 0 || idx >= len(interrupt.Continuation.PendingInterruptions) {
		return NewProtocolError("Code-mode interrupt metadata does not match the signed continuation ledger.", map[string]interface{}{"interruptId": interrupt.InterruptID})
	}
	pending := interrupt.Continuation.PendingInterruptions[idx]
	if pending.InterruptID != interrupt.InterruptID ||
		pending.ToolCallID != interrupt.ToolCallID ||
		pending.ToolName != interrupt.ToolName ||
		!jsonEqual(pending.Input, interrupt.Input) ||
		!jsonEqual(pending.Payload, interrupt.Payload) {
		return NewProtocolError("Code-mode interrupt metadata does not match the signed continuation ledger.", map[string]interface{}{"interruptId": interrupt.InterruptID})
	}
	return nil
}

func jsonEqual(a, b interface{}) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	aj, aerr := json.Marshal(a)
	bj, berr := json.Marshal(b)
	if aerr != nil || berr != nil {
		return false
	}
	var av, bv interface{}
	if err := json.Unmarshal(aj, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(bj, &bv); err != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

// ContinueCodeModeInterrupt resolves a pending Interrupt returned by
// RunCodeMode/Run and resumes execution: RunCodeMode replays the
// interrupt's continuation's recorded source deterministically (see the
// package doc), short-circuiting every already-completed host tool call
// with its recorded result, applies resolution to the call being resumed,
// and continues genuine execution from there. It returns whatever
// RunCodeMode would: the final value, another *Interrupt if execution
// pauses again, or an error. Mirrors TypeScript's
// experimental_continueCodeModeInterrupt.
func ContinueCodeModeInterrupt(ctx context.Context, interrupt Interrupt, resolution interface{}, tools ToolSet, options *Options, toolExecutionOptions *types.ToolExecutionOptions) (interface{}, error) {
	var security ContinuationSecurityOptions
	if options != nil && options.ContinuationSecurity != nil {
		security = *options.ContinuationSecurity
	}
	if err := verifyContinuation(interrupt.Continuation, security); err != nil {
		return nil, err
	}
	if err := assertInterruptMatchesLedger(interrupt); err != nil {
		return nil, err
	}

	continuation := interrupt.Continuation
	return RunCodeMode(ctx, RunInput{
		JS:                   continuation.JS,
		Tools:                tools,
		ToolExecutionOptions: toolExecutionOptions,
		Options:              options,
		Continuation:         &continuation,
		InterruptResolution: &InterruptResolution{
			InterruptID: interrupt.InterruptID,
			Resolution:  resolution,
		},
	})
}

// GetCodeModeInterrupt looks for a pending Interrupt in result: directly
// (result is an Interrupt/*Interrupt/interrupt-shaped map), or nested under
// a "toolResults"/"content" array's entries' "output" field (optionally
// itself wrapped as {"type":"json"|"text","value":...}), matching the
// shapes RunCodeMode's return value and typical tool-result batches take.
// Returns nil if none is found. Mirrors TypeScript's
// experimental_getCodeModeInterrupt.
func GetCodeModeInterrupt(result interface{}, security ...ContinuationSecurityOptions) *Interrupt {
	sec := resolveSecurityArg(security)
	if interrupt := readInterruptValue(result, sec); interrupt != nil {
		return interrupt
	}
	record, ok := result.(map[string]interface{})
	if !ok {
		return nil
	}
	for _, key := range []string{"toolResults", "content"} {
		parts, ok := record[key].([]interface{})
		if !ok {
			continue
		}
		for _, part := range parts {
			partRecord, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			if interrupt := readInterruptValue(partRecord["output"], sec); interrupt != nil {
				return interrupt
			}
		}
	}
	return nil
}

func readInterruptValue(value interface{}, security ContinuationSecurityOptions) *Interrupt {
	if interrupt, ok := asInterrupt(value, security); ok {
		return interrupt
	}
	record, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	typ, _ := record["type"].(string)
	if typ != "json" && typ != "text" {
		return nil
	}
	inner, present := record["value"]
	if !present {
		return nil
	}
	return readInterruptValue(inner, security)
}

// UnwrapCodeModeResult classifies result as completed or interrupted via
// GetCodeModeInterrupt. Mirrors TypeScript's
// experimental_unwrapCodeModeResult.
func UnwrapCodeModeResult(result interface{}, security ...ContinuationSecurityOptions) UnwrappedResult {
	if interrupt := GetCodeModeInterrupt(result, security...); interrupt != nil {
		return UnwrappedResult{Status: "interrupted", Interrupt: interrupt}
	}
	return UnwrappedResult{Status: "completed", Output: result}
}

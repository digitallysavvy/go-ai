package codemode

import (
	"errors"
	"fmt"
)

// Execution limits applied to each sandbox invocation. Zero-value fields
// fall back to their documented defaults when the policy is resolved.
// Mirrors the TypeScript SDK's CodeModeExecutionPolicy
// (code-mode/src/types.ts).
type ExecutionPolicy struct {
	// TimeoutMs bounds wall-clock execution time. Default: 30_000.
	TimeoutMs int

	// CPUTimeoutMs bounds the CPU time sandboxed code uses in one
	// invocation. Time spent waiting on host tools does not count toward it
	// (TimeoutMs, by contrast, includes tool waits). Only takes effect when
	// lower than TimeoutMs. Each continuation resume starts with a fresh
	// budget. Exceeding it fails with *CPUTimeoutError. Counts thread CPU time
	// on Linux, macOS and Windows and sandbox wall time elsewhere. Default:
	// 10_000.
	CPUTimeoutMs int

	// MemoryLimitBytes bounds the sandbox's WASM linear memory. Default:
	// 64 * 1024 * 1024.
	MemoryLimitBytes int

	// MaxStackSizeBytes bounds the JavaScript call stack. Default:
	// 2 * 1024 * 1024.
	MaxStackSizeBytes int

	// MaxResultBytes bounds the JSON-encoded size of the returned value.
	// Default: 1024 * 1024.
	MaxResultBytes int

	// MaxConsoleOutputBytes bounds captured console.* output. Default:
	// 64 * 1024.
	MaxConsoleOutputBytes int

	// MaxSourceBytes bounds the size of the submitted `js` source. Default:
	// 256 * 1024.
	MaxSourceBytes int

	// MaxToolInputBytes bounds the JSON-encoded size of each host tool
	// call's input. Default: 1024 * 1024.
	MaxToolInputBytes int

	// MaxToolOutputBytes bounds the JSON-encoded size of each host tool
	// call's output. Default: 4 * 1024 * 1024.
	MaxToolOutputBytes int

	// MaxBridgeRequests bounds the total number of host tool calls made by
	// one invocation. Default: 256.
	MaxBridgeRequests int

	// MaxInFlightBridgeRequests bounds how many host tool calls may
	// accumulate in one pending-interruption batch at once (see the
	// package doc's "Host tool bridge dispatch" section): calls dispatched
	// concurrently (e.g. within a Promise.all([...])) that all need
	// approval count toward this together. Exceeding it fails the whole
	// invocation with *BridgeLimitError. Default: 32.
	MaxInFlightBridgeRequests int
}

// Default execution policy values, matching the TypeScript SDK's
// code-mode/src/run-code-mode.ts DEFAULT_* constants.
const (
	DefaultTimeoutMs                 = 30_000
	DefaultCPUTimeoutMs              = 10_000
	DefaultMemoryLimitBytes          = 64 * 1024 * 1024
	DefaultMaxStackSizeBytes         = 2 * 1024 * 1024
	DefaultMaxResultBytes            = 1024 * 1024
	DefaultMaxConsoleOutputBytes     = 64 * 1024
	DefaultMaxSourceBytes            = 256 * 1024
	DefaultMaxToolInputBytes         = 1024 * 1024
	DefaultMaxToolOutputBytes        = 4 * 1024 * 1024
	DefaultMaxBridgeRequests         = 256
	DefaultMaxInFlightBridgeRequests = 32
)

// maxRunLimit is the largest value the sandbox runner accepts for a limit
// (TS MAX_RUN_LIMIT, the largest 32-bit signed integer); larger values are
// rejected like zero and negative ones.
const maxRunLimit = 2_147_483_647

// resolvedPolicy is ExecutionPolicy with every field defaulted.
type resolvedPolicy struct {
	TimeoutMs                 int
	CPUTimeoutMs              int
	MemoryLimitBytes          int
	MaxStackSizeBytes         int
	MaxResultBytes            int
	MaxConsoleOutputBytes     int
	MaxSourceBytes            int
	MaxToolInputBytes         int
	MaxToolOutputBytes        int
	MaxBridgeRequests         int
	MaxInFlightBridgeRequests int
}

// resolveExecutionPolicy defaults every unset (zero-value) field of p and
// validates the rest. Mirrors TypeScript's resolveExecutionPolicy
// (code-mode/src/run-code-mode.ts) followed by the `run` package's limit
// validation (normalizeOptions, dist/utils/options.js), whose messages
// code-mode translates from "limits.*" to "executionPolicy.*": a value that
// is not a positive integer fails with "executionPolicy.<name> must be a
// positive integer.", one above 2147483647 with "executionPolicy.<name> must
// be at most 2147483647.", and the fields are checked in `run`'s
// (alphabetical, by run limit name) order, so the first invalid field
// reported is the same as in TS. Its "rejects invalid positive integer
// option" test cases (code-mode/src/utils/options.test.ts) carry one
// deliberate, Go-runtime-forced difference: TS's options are `number |
// undefined`, so it can and does reject an explicit `0`; Go's
// ExecutionPolicy fields are plain `int` with no separate "unset"
// representation, so (per this type's own doc comment) a zero field means
// "use the default" and is not an error -- only a negative value, which can
// never mean "unset", is rejected. TS's non-integer (`1.5`) and `NaN` cases
// have no Go equivalent: `int` cannot hold either.
func resolveExecutionPolicy(p *ExecutionPolicy) (resolvedPolicy, error) {
	r := defaultExecutionPolicy(p)
	if err := validateExecutionPolicy(r); err != nil {
		return resolvedPolicy{}, err
	}
	return r, nil
}

// defaultExecutionPolicy fills every zero field of p with its default
// without validating (TS resolveExecutionPolicy's `??` defaults). RunCodeMode
// validates the result later, where TS's runner does.
func defaultExecutionPolicy(p *ExecutionPolicy) resolvedPolicy {
	r := resolvedPolicy{
		TimeoutMs:                 DefaultTimeoutMs,
		CPUTimeoutMs:              DefaultCPUTimeoutMs,
		MemoryLimitBytes:          DefaultMemoryLimitBytes,
		MaxStackSizeBytes:         DefaultMaxStackSizeBytes,
		MaxResultBytes:            DefaultMaxResultBytes,
		MaxConsoleOutputBytes:     DefaultMaxConsoleOutputBytes,
		MaxSourceBytes:            DefaultMaxSourceBytes,
		MaxToolInputBytes:         DefaultMaxToolInputBytes,
		MaxToolOutputBytes:        DefaultMaxToolOutputBytes,
		MaxBridgeRequests:         DefaultMaxBridgeRequests,
		MaxInFlightBridgeRequests: DefaultMaxInFlightBridgeRequests,
	}
	if p == nil {
		return r
	}
	for _, f := range [...]struct {
		val int
		dst *int
	}{
		{p.TimeoutMs, &r.TimeoutMs},
		{p.CPUTimeoutMs, &r.CPUTimeoutMs},
		{p.MemoryLimitBytes, &r.MemoryLimitBytes},
		{p.MaxStackSizeBytes, &r.MaxStackSizeBytes},
		{p.MaxResultBytes, &r.MaxResultBytes},
		{p.MaxConsoleOutputBytes, &r.MaxConsoleOutputBytes},
		{p.MaxSourceBytes, &r.MaxSourceBytes},
		{p.MaxToolInputBytes, &r.MaxToolInputBytes},
		{p.MaxToolOutputBytes, &r.MaxToolOutputBytes},
		{p.MaxBridgeRequests, &r.MaxBridgeRequests},
		{p.MaxInFlightBridgeRequests, &r.MaxInFlightBridgeRequests},
	} {
		if f.val != 0 {
			*f.dst = f.val
		}
	}
	return r
}

// validateExecutionPolicy checks r in the order `run`'s normalizeOptions
// does (cpuTimeoutMs, maxBridgeRequests, maxConsoleOutputBytes,
// maxHostFunctionArgumentsBytes, maxHostFunctionOutputBytes,
// maxInFlightBridgeRequests, maxResultBytes, maxSourceBytes,
// maxStackSizeBytes, memoryLimitBytes, timeoutMs), naming each field by its
// code-mode name as TS's translateLimitPath does.
func validateExecutionPolicy(r resolvedPolicy) error {
	for _, f := range [...]struct {
		name string
		val  int
	}{
		{"cpuTimeoutMs", r.CPUTimeoutMs},
		{"maxBridgeRequests", r.MaxBridgeRequests},
		{"maxConsoleOutputBytes", r.MaxConsoleOutputBytes},
		{"maxToolInputBytes", r.MaxToolInputBytes},
		{"maxToolOutputBytes", r.MaxToolOutputBytes},
		{"maxInFlightBridgeRequests", r.MaxInFlightBridgeRequests},
		{"maxResultBytes", r.MaxResultBytes},
		{"maxSourceBytes", r.MaxSourceBytes},
		{"maxStackSizeBytes", r.MaxStackSizeBytes},
		{"memoryLimitBytes", r.MemoryLimitBytes},
		{"timeoutMs", r.TimeoutMs},
	} {
		if f.val <= 0 {
			return runTypeError("executionPolicy.%s must be a positive integer.", f.name)
		}
		if int64(f.val) > maxRunLimit {
			return runTypeError("executionPolicy.%s must be at most %d.", f.name, maxRunLimit)
		}
	}
	return nil
}

// isValidRunLimit mirrors TS isValidRunLimit (run-code-mode.ts).
func isValidRunLimit(value int) bool {
	return value > 0 && int64(value) <= maxRunLimit
}

// runTypeError builds the plain error for one of `run`'s TypeErrors, keeping
// TS's text (with its final period; staticcheck ST1005 only inspects literal
// error constructors).
func runTypeError(format string, args ...interface{}) error {
	msg := fmt.Sprintf(format, args...)
	return errors.New(msg)
}

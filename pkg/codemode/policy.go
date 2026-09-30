package codemode

import "fmt"

// Execution limits applied to each sandbox invocation. Zero-value fields
// fall back to their documented defaults when the policy is resolved.
// Mirrors the TypeScript SDK's CodeModeExecutionPolicy
// (code-mode/src/types.ts).
type ExecutionPolicy struct {
	// TimeoutMs bounds wall-clock execution time. Default: 30_000.
	TimeoutMs int

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

// resolvedPolicy is ExecutionPolicy with every field defaulted.
type resolvedPolicy struct {
	TimeoutMs                 int
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
// (code-mode/src/run-code-mode.ts) and its "rejects invalid positive
// integer option" test cases (code-mode/src/utils/options.test.ts), with
// one deliberate, Go-runtime-forced difference: TS's options are `number |
// undefined`, so it can and does reject an explicit `0` ("must be a
// positive integer"); Go's ExecutionPolicy fields are plain `int` with no
// separate "unset" representation, so (per this type's own doc comment) a
// zero field means "use the default" and is not an error -- only a
// negative value, which can never mean "unset", is rejected. TS's
// non-integer (`1.5`) and `NaN` cases have no Go equivalent: `int` cannot
// hold either.
func resolveExecutionPolicy(p *ExecutionPolicy) (resolvedPolicy, error) {
	r := resolvedPolicy{
		TimeoutMs:                 DefaultTimeoutMs,
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
		return r, nil
	}

	fields := [...]struct {
		name string
		val  int
		dst  *int
	}{
		{"timeoutMs", p.TimeoutMs, &r.TimeoutMs},
		{"memoryLimitBytes", p.MemoryLimitBytes, &r.MemoryLimitBytes},
		{"maxStackSizeBytes", p.MaxStackSizeBytes, &r.MaxStackSizeBytes},
		{"maxResultBytes", p.MaxResultBytes, &r.MaxResultBytes},
		{"maxConsoleOutputBytes", p.MaxConsoleOutputBytes, &r.MaxConsoleOutputBytes},
		{"maxSourceBytes", p.MaxSourceBytes, &r.MaxSourceBytes},
		{"maxToolInputBytes", p.MaxToolInputBytes, &r.MaxToolInputBytes},
		{"maxToolOutputBytes", p.MaxToolOutputBytes, &r.MaxToolOutputBytes},
		{"maxBridgeRequests", p.MaxBridgeRequests, &r.MaxBridgeRequests},
		{"maxInFlightBridgeRequests", p.MaxInFlightBridgeRequests, &r.MaxInFlightBridgeRequests},
	}
	for _, f := range fields {
		if f.val < 0 {
			return resolvedPolicy{}, fmt.Errorf("codemode: executionPolicy.%s must be a positive integer, got %d", f.name, f.val)
		}
		if f.val > 0 {
			*f.dst = f.val
		}
	}
	return r, nil
}

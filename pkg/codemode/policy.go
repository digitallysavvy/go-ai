package codemode

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

	// MaxInFlightBridgeRequests bounds concurrently outstanding host tool
	// calls. This Go port dispatches host tool calls synchronously (see
	// package doc), so this limit is accepted for API parity but never
	// exceeded in practice. Default: 32.
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

func resolveExecutionPolicy(p *ExecutionPolicy) resolvedPolicy {
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
		return r
	}
	if p.TimeoutMs > 0 {
		r.TimeoutMs = p.TimeoutMs
	}
	if p.MemoryLimitBytes > 0 {
		r.MemoryLimitBytes = p.MemoryLimitBytes
	}
	if p.MaxStackSizeBytes > 0 {
		r.MaxStackSizeBytes = p.MaxStackSizeBytes
	}
	if p.MaxResultBytes > 0 {
		r.MaxResultBytes = p.MaxResultBytes
	}
	if p.MaxConsoleOutputBytes > 0 {
		r.MaxConsoleOutputBytes = p.MaxConsoleOutputBytes
	}
	if p.MaxSourceBytes > 0 {
		r.MaxSourceBytes = p.MaxSourceBytes
	}
	if p.MaxToolInputBytes > 0 {
		r.MaxToolInputBytes = p.MaxToolInputBytes
	}
	if p.MaxToolOutputBytes > 0 {
		r.MaxToolOutputBytes = p.MaxToolOutputBytes
	}
	if p.MaxBridgeRequests > 0 {
		r.MaxBridgeRequests = p.MaxBridgeRequests
	}
	if p.MaxInFlightBridgeRequests > 0 {
		r.MaxInFlightBridgeRequests = p.MaxInFlightBridgeRequests
	}
	return r
}

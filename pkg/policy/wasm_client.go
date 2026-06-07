package policy

import (
	"context"
	"errors"
)

// LoadedWASMPolicy mirrors the minimal behavior used from @open-policy-agent/opa-wasm.
type LoadedWASMPolicy interface {
	Evaluate(input any) ([]WASMPolicyResult, error)
}

// WASMPolicyResult is one top-level OPA WASM evaluation result.
type WASMPolicyResult struct {
	Result any
}

// WASMClientOptions configures WASMPolicyClient.
type WASMClientOptions struct {
	Data       any
	LoadPolicy func(wasm []byte, data any) (LoadedWASMPolicy, error)
}

type wasmPolicyClient struct {
	policy LoadedWASMPolicy
}

// WASMPolicyClient constructs an in-process OPA WASM policy client.
func WASMPolicyClient(wasmBytes []byte, opts ...WASMClientOptions) (PolicyClient, error) {
	cfg := WASMClientOptions{}
	if len(opts) > 0 {
		cfg = opts[0]
	}
	if cfg.LoadPolicy == nil {
		cfg.LoadPolicy = defaultLoadWASMPolicy
	}
	policy, err := cfg.LoadPolicy(append([]byte(nil), wasmBytes...), cfg.Data)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, errors.New("OPA WASM policy loader returned nil policy")
	}
	return &wasmPolicyClient{policy: policy}, nil
}

func (c *wasmPolicyClient) Evaluate(ctx context.Context, policyPath string, input any) (any, error) {
	results, err := c.policy.Evaluate(input)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, errors.New("OPA WASM policy produced no result. Check that the bundle was built with the correct entrypoint (`opa build -t wasm -e <path>`).")
	}
	return results[0].Result, nil
}

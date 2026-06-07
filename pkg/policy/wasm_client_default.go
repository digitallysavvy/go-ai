package policy

import "errors"

func defaultLoadWASMPolicy(wasm []byte, data any) (LoadedWASMPolicy, error) {
	return nil, errors.New("Cannot import \"@open-policy-agent/opa-wasm\". Provide WASMClientOptions.LoadPolicy to use WASMPolicyClient().")
}

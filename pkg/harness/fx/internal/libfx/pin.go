// Package libfx hosts the libfx core, fx-core.wasm, on wazero. It is the Go
// counterpart of the wasm backend in the libfx npm package (fx-sdk.js
// createRuntime and createFxEngine): a minimal WASI preview1, the "fx" host
// module, the ACP JSON-RPC channel over the core's stdin and stdout, and the
// download, verification and caching of the pinned module.
//
// The pin lives in pin_gen.go, written by genpin. To move to a new libfx
// release (match the "libfx" dependency of TS @ai-sdk/harness-fx):
//
//	go run ./pkg/harness/fx/internal/libfx/genpin -version 0.0.15
//
// Then review the expectedImports diff, the host import table in the new
// fx-sdk.js (createRuntime), and run the real-wasm tests:
//
//	GOAI_FX_WASM_TEST=1 go test -race -p 2 ./pkg/harness/fx/...
//
// Portions of this package port logic from libfx (Apache-2.0, Copyright
// Vercel, Inc.); see NOTICE. The wasm module itself is not redistributed.
package libfx

import "github.com/tetratelabs/wazero/api"

//go:generate go run ./genpin -out pin_gen.go

// importSig is one function import of fx-core.wasm.
type importSig struct {
	Module  string
	Name    string
	Params  []api.ValueType
	Results []api.ValueType
}

package codemode

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/third_party/qjs"
)

// TestEngineRejectsDynamicImport_WithStaticCheckBypassed is the
// defense-in-depth regression test for BF1b (build/disable-modules.patch,
// pkg/internal/third_party/qjs). RunCodeMode itself always runs
// assertNoDynamicImport (run_code_mode.go) before any source reaches the
// engine, so every other test in this package that drives
// `import('qjs:std')` etc. through RunCodeMode is only proving the
// Go-level static scan works -- it is never actually reaching qjs.wasm's
// module loader at all.
//
// That static scan has already been bypassed twice in production (a bare
// 'qjs:' specifier, then a template-literal interpolation -- see
// dynamic_import_test.go and dynamic_import_template_test.go for the
// regression coverage of those two). BF1b's premise is that the engine
// itself must refuse every import regardless of whether a third bypass of
// the Go-side scan is ever found.
//
// This test proves that premise directly: it builds the exact sandbox
// RunCodeMode's runInSandbox constructs in production (same resolved
// policy, same NoFSMount/DisableModules option, same stripSandboxGlobals/
// installRuntimeHardening calls -- see engine.go), but skips
// assertNoDynamicImport entirely and hands the engine a literal
// `import('qjs:std')` directly. If this ever starts succeeding, the
// engine-level defense has regressed even though every RunCodeMode-level
// test in this package would keep passing (since they never stop relying
// on the Go-side check in the first place).
func TestEngineRejectsDynamicImport_WithStaticCheckBypassed(t *testing.T) {
	policy, perr := resolveExecutionPolicy(nil)
	if perr != nil {
		t.Fatalf("resolveExecutionPolicy: %v", perr)
	}

	specifiers := []string{"qjs:std", "qjs:os", "qjs:bjson", "./x", "/x"}
	for _, spec := range specifiers {
		spec := spec
		t.Run(spec, func(t *testing.T) {
			// drive runs on a goroutine runInSandbox spawns internally, so
			// every assertion below is reported via a returned error
			// rather than t.Fatalf (unsafe to call off the test's own
			// goroutine) and checked after runInSandbox returns.
			_, _, _, err := runInSandbox(context.Background(), policy, func(jsCtx *qjs.Context) (string, bool, bool, error) {
				p, eerr := jsCtx.EvalNoAutoAwait("probe.js", qjs.Code(`import('`+spec+`')`))
				if eerr != nil {
					return "", false, false, eerr
				}
				if !p.IsPromise() {
					return "", false, false, fmt.Errorf("expected import(%q) to produce a promise even with the static check bypassed", spec)
				}
				if _, rerr := jsCtx.RunPendingJobs(); rerr != nil {
					return "", false, false, rerr
				}
				if got := p.PromiseState(); got != qjs.PromiseStateRejected {
					return "", false, false, fmt.Errorf("expected the engine itself to reject import(%q) with the static check bypassed, got state %v", spec, got)
				}
				reason := p.PromiseResult().Exception()
				if reason == nil || !strings.Contains(reason.Error(), "disabled") {
					return "", false, false, fmt.Errorf("expected a 'disabled' rejection reason for import(%q), got %v", spec, reason)
				}
				return "", true, false, nil
			})
			if err != nil {
				t.Fatalf("defense-in-depth check failed: %v", err)
			}
		})
	}
}

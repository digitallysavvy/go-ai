package qjs

import (
	"context"
	"strings"
	"testing"
)

// TestDisableModules_RejectsEveryImportSpecifier is the engine-level
// regression test for build/disable-modules.patch (see README.vendor.md's
// "Native-module import escape" section): with Option.DisableModules set,
// a dynamic `import(...)` of any specifier -- the three native
// quickjs-libc host-escape modules (qjs:std/qjs:os/qjs:bjson) as well as
// ordinary relative/absolute file specifiers -- must reject with a clean
// JS error, and the qjs:std/qjs:os/qjs:bjson module namespace objects must
// never be obtainable.
//
// This exercises the engine (qjs.wasm) directly, deliberately bypassing
// pkg/codemode's Go-level static scan (assertNoDynamicImport) entirely --
// it calls EvalNoAutoAwait with a literal `import(...)` expression, the
// same bypass a model's code could construct if that static check were
// ever defeated again (as it already has been twice: a bare 'qjs:'
// specifier, then a template-literal interpolation -- see
// pkg/codemode/sandbox_escape_routes_test.go and
// dynamic_import_template_test.go for the Go-level regression coverage of
// those). The point of this patch is that the static check is no longer
// the only thing standing between sandboxed code and these modules.
func TestDisableModules_RejectsEveryImportSpecifier(t *testing.T) {
	specifiers := []string{"qjs:std", "qjs:os", "qjs:bjson", "./x", "/x"}

	for _, spec := range specifiers {
		t.Run(spec, func(t *testing.T) {
			rt, err := New(Option{
				Context:        context.Background(),
				NoFSMount:      true,
				DisableModules: true,
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer rt.Close()

			jsCtx := rt.Context()

			p, err := jsCtx.EvalNoAutoAwait("probe.js", Code(`import('`+spec+`')`))
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			if !p.IsPromise() {
				t.Fatalf("expected import(%q) to produce a promise", spec)
			}

			if _, rerr := rt.RunPendingJobs(); rerr != nil {
				t.Fatalf("RunPendingJobs: %v", rerr)
			}

			if got := p.PromiseState(); got != PromiseStateRejected {
				t.Fatalf("expected import(%q) to reject with DisableModules set, got state %v", spec, got)
			}

			reason := p.PromiseResult()
			rerr := reason.Exception()
			if rerr == nil {
				t.Fatalf("expected a non-nil rejection reason for import(%q)", spec)
			}
			if !strings.Contains(rerr.Error(), "disabled") {
				t.Fatalf("expected the rejection reason for import(%q) to mention the import being disabled, got %q", spec, rerr.Error())
			}

			// The native modules must never have been registered at all --
			// not merely rejected on import -- so there is no backdoor via
			// a name that happens to already be loaded. globalThis.std
			// (etc.) must also never have been set, mirroring
			// js_set_global_objs's own disable-modules guard in
			// build/disable-modules.patch.
			for _, name := range []string{"std", "os", "bjson"} {
				g := jsCtx.Global().GetPropertyStr(name)
				if !g.IsUndefined() {
					t.Fatalf("expected global %q to remain undefined with DisableModules set, got type %v", name, g.Type())
				}
			}
		})
	}
}

// TestDisableModules_DefaultBehaviorUnchanged confirms Option.DisableModules
// defaults to false and, when left unset, does not alter this vendored
// package's existing behavior for non-code-mode consumers: qjs:std (and,
// by the same code path, qjs:os/qjs:bjson) remain importable and usable
// exactly as before this patch.
func TestDisableModules_DefaultBehaviorUnchanged(t *testing.T) {
	rt, err := New(Option{Context: context.Background()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer rt.Close()

	jsCtx := rt.Context()

	p, err := jsCtx.EvalNoAutoAwait("probe.js", Code(`import('qjs:std')`))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !p.IsPromise() {
		t.Fatalf("expected import('qjs:std') to produce a promise")
	}

	if _, rerr := rt.RunPendingJobs(); rerr != nil {
		t.Fatalf("RunPendingJobs: %v", rerr)
	}

	if got := p.PromiseState(); got != PromiseStateFulfilled {
		reason := "n/a"
		if got == PromiseStateRejected {
			reason = p.PromiseResult().Exception().Error()
		}
		t.Fatalf("expected import('qjs:std') to fulfill with DisableModules left at its default, got state %v (reason: %s)", got, reason)
	}

	mod := p.PromiseResult()
	getenv := mod.GetPropertyStr("getenv")
	if getenv.IsUndefined() {
		t.Fatalf("expected the qjs:std module namespace to expose getenv, as before this patch")
	}

	// globalThis.std is also still wired up by js_set_global_objs when
	// modules are not disabled -- unchanged from pre-patch behavior.
	g := jsCtx.Global().GetPropertyStr("std")
	if g.IsUndefined() {
		t.Fatalf("expected globalThis.std to remain set when DisableModules is left at its default")
	}
}

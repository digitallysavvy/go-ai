//go:build ignore
// +build ignore

// This example shows how the fx harness adapter is configured on its two
// runtimes, without network access or an installed CLI:
//
//   - the default sandbox-ACP runtime (fx.RuntimeSandboxACP, the go-ai
//     v0.5.x behavior): fx.CreateFx() with no Runtime, the ACP recipe
//     fx.BuildConfig assembles, and the v0.5.x ChatGPT/Grok subscription
//     reader (ReadSubscriptions reads, and may refresh, real fx credentials
//     from $HOME/.fx if present; with none it returns no subscription);
//   - the opt-in libfx runtime (fx.RuntimeLibfx, the TS @ai-sdk/harness-fx
//     runtime): AI Gateway or codex/grok subscription authentication, HTTP
//     or SSE MCP servers, and the fx-core.wasm cache and offline settings.
//     Building it downloads nothing; the first session (or fx.PrepareWasm)
//     resolves the pinned module.
//
// Driving an actual sandbox-ACP session (HARNESS_E2E=1) additionally
// requires Node.js and pnpm on PATH (the sandbox installs the fx CLI on
// first run) and a way for the sandboxed `fx` CLI to authenticate. See
// fx_libfx.go for a libfx session.
//
// Run with: go run ./examples/harness/fx_config.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/fx"
	"github.com/digitallysavvy/go-ai/pkg/harness/sandbox/local"
)

func main() {
	h, err := fx.CreateFx()
	if err != nil {
		log.Fatalf("fx.CreateFx: %v", err)
	}

	fmt.Printf("harnessId:  %s\n", h.HarnessID())
	fmt.Printf("builtin tool count: %d\n", len(h.BuiltinTools()))

	cfg := fx.BuildConfig(fx.Settings{})
	fmt.Printf("executable: %s %v\n", cfg.Executable, cfg.Args)
	fmt.Printf("instructionMapping: %+v\n", cfg.InstructionMapping)
	fmt.Printf("credentialEnv: %v\n", cfg.CredentialEnv)

	subs, err := fx.ReadSubscriptions(context.Background(), fx.ReadSubscriptionsOptions{})
	if err != nil {
		fmt.Printf("ReadSubscriptions error: %v\n", err)
	} else if subs == nil {
		fmt.Println("no native fx subscription found under ~/.fx (expected on a machine without fx installed)")
	} else {
		fmt.Printf("found a native fx subscription for: %v\n", keys(subs))
	}

	// The libfx runtime. Auth "direct" prefers a codex or grok subscription
	// found on this host (~/.fx, $CODEX_HOME or the OS keyring, $GROK_HOME)
	// over AI_GATEWAY_API_KEY; the default ("auto") prefers the Gateway key.
	libfx, err := fx.CreateFx(fx.Settings{
		Runtime: fx.RuntimeLibfx,
		Auth:    harness.AuthMode(harness.AuthModeDirect),
		// HTTP (streamable) and SSE MCP servers; their tools reach the model
		// as mcp_<server>_<tool>. Command (stdio) servers are not supported.
		MCPServers: map[string]any{
			"context7": map[string]any{"url": "https://mcp.context7.com/mcp", "type": "http"},
		},
		ServiceTier: fx.ServiceTierFast,
		// Offline hosts: point Path at a local fx-core.wasm (or pre-seed the
		// cache) and set Offline, or set GO_AI_FX_WASM_PATH and
		// GO_AI_FX_OFFLINE=1 in the environment.
		Wasm: &fx.WasmSource{Offline: os.Getenv("GO_AI_FX_OFFLINE") == "1"},
	})
	if err != nil {
		log.Fatalf("fx.CreateFx (libfx): %v", err)
	}
	fmt.Printf("libfx runtime: libfx %s (fx-core.wasm sha256 %s), %d builtin tools\n", fx.LibfxVersion, fx.LibfxWasmSHA256[:12], len(libfx.BuiltinTools()))

	if os.Getenv("HARNESS_E2E") != "1" {
		fmt.Println("Set HARNESS_E2E=1 (with Node/pnpm on PATH and fx CLI credentials) to run a live session.")
		return
	}

	agent, err := harness.NewAgent(harness.AgentSettings{
		Harness:        h,
		Model:          "auto",
		Sandbox:        local.NewProvider(local.Options{Ports: []int{4318}}), // the harness bridge listens on this port
		PermissionMode: harness.PermissionModeAllowAll,
	})
	if err != nil {
		log.Fatalf("harness.NewAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result, err := agent.Execute(ctx, "List the files in the current directory, then say hello.")
	if err != nil {
		log.Fatalf("Execute: %v", err)
	}
	fmt.Println("=== fx response ===")
	fmt.Println(result.Text)
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

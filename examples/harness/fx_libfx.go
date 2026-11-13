//go:build ignore
// +build ignore

// This example runs the fx harness on the libfx runtime: the libfx core
// (fx-core.wasm) runs in this process, and its built-in tools run in the
// session sandbox. The first run downloads and caches the pinned module;
// fx.PrepareWasm does that up front.
//
// It needs AI_GATEWAY_API_KEY (or VERCEL_OIDC_TOKEN) and a Linux sandbox,
// because the built-in tools use GNU realpath, base64 and find. Without a
// key it only prints the harness metadata.
//
// Run with: go run ./examples/harness/fx_libfx.go
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
	h, err := fx.CreateFx(fx.Settings{Runtime: fx.RuntimeLibfx})
	if err != nil {
		log.Fatalf("fx.CreateFx: %v", err)
	}
	fmt.Printf("harnessId: %s, libfx %s\n", h.HarnessID(), fx.LibfxVersion)
	for _, name := range []string{"read", "write", "edit", "bash", "grep", "glob", "ls", "askUserQuestions"} {
		fmt.Printf("  builtin %s (%s)\n", name, h.BuiltinTools()[name].ToolUseKind)
	}
	if os.Getenv("AI_GATEWAY_API_KEY") == "" && os.Getenv("VERCEL_OIDC_TOKEN") == "" {
		fmt.Println("Set AI_GATEWAY_API_KEY to run a session.")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := fx.PrepareWasm(ctx, nil); err != nil {
		log.Fatalf("fx.PrepareWasm: %v", err)
	}
	agent, err := harness.NewAgent(harness.AgentSettings{
		Harness:        h,
		Model:          "anthropic/claude-haiku-4.5",
		Sandbox:        local.NewProvider(local.Options{}),
		PermissionMode: harness.PermissionModeAllowAll,
	})
	if err != nil {
		log.Fatalf("harness.NewAgent: %v", err)
	}
	result, err := agent.Execute(ctx, "List the files in the workspace, then say hello.")
	if err != nil {
		log.Fatalf("Execute: %v", err)
	}
	fmt.Println(result.Text)
}

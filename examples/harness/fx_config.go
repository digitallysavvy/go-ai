//go:build ignore
// +build ignore

// This example builds a live harness.Harness for the fx CLI harness adapter
// via fx.CreateFx (the real constructor, wired through pkg/harness/acp) and
// prints the ACP recipe pkg/harness/fx assembles, plus demonstrates the
// native ChatGPT/Grok subscription reader. Building the harness and
// inspecting its metadata requires no network access or installed CLI;
// ReadSubscriptions reads (and may refresh) real fx credentials from
// $HOME/.fx if present, so it is safe to run with an empty or missing
// directory (it just returns no subscription).
//
// Driving an actual session (HARNESS_E2E=1) additionally requires Node.js
// and pnpm on PATH (the sandbox installs the fx CLI on first run) and a way
// for the sandboxed `fx` CLI to authenticate.
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

	if os.Getenv("HARNESS_E2E") != "1" {
		fmt.Println("Set HARNESS_E2E=1 (with Node/pnpm on PATH and fx CLI credentials) to run a live session.")
		return
	}

	agent, err := harness.NewAgent(harness.AgentSettings{
		Harness:        h,
		Model:          "auto",
		Sandbox:        local.NewProvider(local.Options{}),
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

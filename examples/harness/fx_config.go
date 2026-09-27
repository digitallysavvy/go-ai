//go:build ignore
// +build ignore

// This example prints the ACP recipe pkg/harness/fx assembles for the fx
// CLI harness adapter, and demonstrates the native ChatGPT/Grok
// subscription reader. It requires no network access or installed CLI for
// the recipe portion; ReadSubscriptions reads (and may refresh) real fx
// credentials from $HOME/.fx if present, so it is safe to run with an empty
// or missing directory (it just returns no subscription).
//
// See pkg/harness/cursor's package doc for why this stops at BuildConfig
// instead of a live harness.Harness (pending pkg/harness/acp, WG11).
//
// Run with: go run ./examples/harness/fx_config.go
package main

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/harness/fx"
)

func main() {
	cfg := fx.BuildConfig(fx.Settings{})

	fmt.Printf("harnessId:  %s\n", cfg.HarnessID)
	fmt.Printf("executable: %s %v\n", cfg.Executable, cfg.Args)
	fmt.Printf("instructionMapping: %+v\n", cfg.InstructionMapping)
	fmt.Printf("credentialEnv: %v\n", cfg.CredentialEnv)
	fmt.Printf("builtin tool count: %d\n", len(cfg.BuiltinTools))

	subs, err := fx.ReadSubscriptions(context.Background(), fx.ReadSubscriptionsOptions{})
	if err != nil {
		fmt.Printf("ReadSubscriptions error: %v\n", err)
		return
	}
	if subs == nil {
		fmt.Println("no native fx subscription found under ~/.fx (expected on a machine without fx installed)")
		return
	}
	fmt.Printf("found a native fx subscription for: %v\n", keys(subs))
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

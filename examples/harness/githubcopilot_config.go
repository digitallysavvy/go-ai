//go:build ignore
// +build ignore

// This example prints the ACP recipe pkg/harness/githubcopilot assembles
// for the GitHub Copilot CLI harness adapter, including the embedded
// npm-locked install recipe (package.json/pnpm-lock.yaml synced by WG6) and
// a JSONC round-trip through the adapter's own config reader.
//
// See pkg/harness/cursor's package doc for why this stops at BuildConfig
// instead of a live harness.Harness (pending pkg/harness/acp, WG11).
//
// Run with: go run ./examples/harness/githubcopilot_config.go
package main

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/harness/githubcopilot"
)

func main() {
	cfg, err := githubcopilot.BuildConfig(githubcopilot.Settings{
		ReasoningEffort: githubcopilot.ReasoningEffortHigh,
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("harnessId:  %s\n", cfg.HarnessID)
	fmt.Printf("executable: %s %v\n", cfg.Executable, cfg.Args)
	fmt.Printf("source type: %s (package.json is %d bytes)\n", cfg.Source.Type, len(cfg.Source.PackageJSON))
	fmt.Printf("hostToolMcpTransport: %s\n", cfg.HostToolMCPTransport)
	fmt.Printf("builtin tool count: %d\n", len(cfg.BuiltinTools))

	// Copilot CLI's own config.json is JSONC (comments + trailing commas).
	// githubcopilot.ParseJSONC is a small, dependency-free reader for it.
	sample := `{
		// synced by the Copilot CLI on login
		"lastLoggedInUser": { "host": "https://github.com", "login": "octocat" },
	}`
	parsed, err := githubcopilot.ParseJSONC([]byte(sample))
	if err != nil {
		panic(err)
	}
	fmt.Printf("parsed JSONC config: %v\n", parsed)
}

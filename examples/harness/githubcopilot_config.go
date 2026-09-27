//go:build ignore
// +build ignore

// This example builds a live harness.Harness for the GitHub Copilot CLI
// harness adapter via githubcopilot.CreateGitHubCopilot (the real
// constructor, wired through pkg/harness/acp) and prints the ACP recipe
// pkg/harness/githubcopilot assembles, including the embedded npm-locked
// install recipe (package.json/pnpm-lock.yaml synced by WG6) and a JSONC
// round-trip through the adapter's own config reader.
//
// Driving an actual session (HARNESS_E2E=1) additionally requires Node.js
// and pnpm on PATH (the sandbox installs the `copilot` CLI on first run)
// and a way for the sandboxed CLI to authenticate (COPILOT_GITHUB_TOKEN,
// GH_TOKEN, GITHUB_TOKEN, or a prior `gh auth login` / Copilot CLI login).
//
// Run with: go run ./examples/harness/githubcopilot_config.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/githubcopilot"
	"github.com/digitallysavvy/go-ai/pkg/harness/sandbox/local"
)

func main() {
	settings := githubcopilot.Settings{ReasoningEffort: githubcopilot.ReasoningEffortHigh}

	h, err := githubcopilot.CreateGitHubCopilot(settings)
	if err != nil {
		log.Fatalf("githubcopilot.CreateGitHubCopilot: %v", err)
	}

	fmt.Printf("harnessId:  %s\n", h.HarnessID())
	fmt.Printf("builtin tool count: %d\n", len(h.BuiltinTools()))

	cfg, err := githubcopilot.BuildConfig(settings)
	if err != nil {
		log.Fatalf("githubcopilot.BuildConfig: %v", err)
	}
	fmt.Printf("executable: %s %v\n", cfg.Executable, cfg.Args)
	fmt.Printf("source type: %s (package.json is %d bytes)\n", cfg.Source.Type, len(cfg.Source.PackageJSON))
	fmt.Printf("hostToolMcpTransport: %s\n", cfg.HostToolMCPTransport)

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

	if os.Getenv("HARNESS_E2E") != "1" {
		fmt.Println("Set HARNESS_E2E=1 (with Node/pnpm on PATH and Copilot CLI credentials) to run a live session.")
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
	fmt.Println("=== GitHub Copilot response ===")
	fmt.Println(result.Text)
}

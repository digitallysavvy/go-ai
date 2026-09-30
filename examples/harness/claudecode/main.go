//go:build ignore

// This example drives the Claude Code coding-agent runtime through the Go
// harness host (pkg/harness/claudecode), using the local sandbox provider
// (pkg/harness/sandbox/local) so no Vercel Sandbox account is required.
//
// Requirements:
//   - Node.js >= 20 and pnpm on PATH (the sandbox installs the embedded
//     bridge's dependencies, including the `claude` CLI, on first run).
//   - ANTHROPIC_API_KEY (or AI_GATEWAY_API_KEY / CLAUDE_CODE_OAUTH_TOKEN) set
//     in the environment.
//
// Run with: go run examples/harness/claudecode/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
	"github.com/digitallysavvy/go-ai/pkg/harness/sandbox/local"
)

func main() {
	if os.Getenv("HARNESS_E2E") != "1" {
		fmt.Println("Set HARNESS_E2E=1 (and ANTHROPIC_API_KEY, with Node/pnpm on PATH) to run this example.")
		return
	}

	cc, err := claudecode.New(claudecode.Settings{
		MaxTurns: 8,
	})
	if err != nil {
		log.Fatalf("claudecode.New: %v", err)
	}

	sandbox := local.NewProvider(local.Options{})

	agent, err := harness.NewAgent(harness.AgentSettings{
		Harness:        cc,
		Model:          "claude-sonnet-4-5",
		Sandbox:        sandbox,
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
	fmt.Println("=== Claude Code response ===")
	fmt.Println(result.Text)
}

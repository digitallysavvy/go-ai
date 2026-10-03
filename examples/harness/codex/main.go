//go:build ignore

// This example drives the Codex coding-agent runtime through the Go harness
// host (pkg/harness/codex), using the local sandbox provider
// (pkg/harness/sandbox/local) so no Vercel Sandbox account is required.
//
// Requirements:
//   - Node.js >= 20 and pnpm on PATH (the sandbox installs the embedded
//     bridge's dependencies, including the `codex` CLI, on first run).
//   - OPENAI_API_KEY (or AI_GATEWAY_API_KEY / CODEX_API_KEY) set in the
//     environment.
//
// Run with: go run examples/harness/codex/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/codex"
	"github.com/digitallysavvy/go-ai/pkg/harness/sandbox/local"
)

func main() {
	if os.Getenv("HARNESS_E2E") != "1" {
		fmt.Println("Set HARNESS_E2E=1 (and OPENAI_API_KEY, with Node/pnpm on PATH) to run this example.")
		return
	}

	cx := codex.New(codex.Settings{})

	sandbox := local.NewProvider(local.Options{Ports: []int{4318}}) // the harness bridge listens on this port

	agent, err := harness.NewAgent(harness.AgentSettings{
		Harness:        cx,
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
	fmt.Println("=== Codex response ===")
	fmt.Println(result.Text)
}

//go:build ignore
// +build ignore

// This example creates a real Vercel Sandbox via
// pkg/harness/sandbox/vercel, runs a command in it, and destroys it.
// Mirrors ai/examples/ai-functions/src/harness-agent/cline/
// with-provided-sandbox.ts and prepare-sandbox-for-harness.ts, using the Go
// port's function-based API (CreateNetworkSandboxSession) instead of
// @vercel/sandbox's `Sandbox.create` + createVercelNetworkSandboxSession
// FromNativeSandbox wrapper.
//
// Requires Vercel Sandbox credentials: either VERCEL_OIDC_TOKEN in the
// environment (set by `vercel env pull` when the project is linked), or all
// three of VERCEL_TOKEN, VERCEL_TEAM_ID, and VERCEL_PROJECT_ID. Without
// credentials, this prints a message and exits 0 rather than failing --
// useful for `go run` smoke-checking the build without live access.
//
// Run with: go run ./examples/harness/vercel_sandbox.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/harness/sandbox/vercel"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

func main() {
	creds := vercel.Credentials{
		Token:     os.Getenv("VERCEL_TOKEN"),
		TeamID:    os.Getenv("VERCEL_TEAM_ID"),
		ProjectID: os.Getenv("VERCEL_PROJECT_ID"),
	}
	if !vercel.HasConfiguredCredentials(creds) {
		fmt.Println("Skipping: no Vercel Sandbox credentials configured.")
		fmt.Println("Set VERCEL_OIDC_TOKEN, or VERCEL_TOKEN + VERCEL_TEAM_ID + VERCEL_PROJECT_ID, to run this example live.")
		return
	}

	ctx := context.Background()

	session, err := vercel.CreateNetworkSandboxSession(ctx, vercel.CreateSessionOptions{
		Credentials: creds,
		Runtime:     "node24",
	})
	if err != nil {
		log.Fatalf("vercel.CreateNetworkSandboxSession: %v", err)
	}
	defer func() {
		if err := session.Destroy(ctx); err != nil {
			log.Printf("session.Destroy: %v", err)
		}
	}()

	fmt.Printf("sandbox id: %s\n", session.ID())
	fmt.Printf("default working directory: %s\n", session.DefaultWorkingDirectory())

	result, err := session.Run(ctx, providerutils.SandboxProcessOptions{Command: "echo hello from vercel sandbox"})
	if err != nil {
		log.Fatalf("session.Run: %v", err)
	}
	fmt.Printf("exit code: %d\n", result.ExitCode)
	fmt.Printf("stdout: %s", result.Stdout)
}

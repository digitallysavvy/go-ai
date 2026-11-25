// Command toolbash demonstrates pkg/toolbash: a provider-independent bash
// tool that runs the model's commands in a sandbox session you supply.
//
// This example needs no API key or network access. It uses the local
// harness sandbox, which has NO isolation (commands run as your user), so run
// it only on a machine you trust. In production, pass an isolated sandbox
// such as pkg/harness/sandbox/vercel. The tool runs nothing unless you give it
// a sandbox.
//
// Run with: go run ./examples/toolbash
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/sandbox/local"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/toolbash"
)

func main() {
	ctx := context.Background()

	root, err := os.MkdirTemp("", "toolbash-example-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(root) //nolint:errcheck

	// Only PATH is inherited, so the commands cannot read your API keys.
	provider := local.NewProvider(local.Options{RootDir: root, HostEnv: []string{"PATH"}})
	session, err := provider.CreateSession(ctx, harness.CreateSandboxSessionOptions{SessionID: "bash-example"})
	if err != nil {
		log.Fatal(err)
	}
	defer session.Destroy(ctx) //nolint:errcheck

	sandbox := session.Restricted()
	if err := sandbox.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{
		Path:    "notes.txt",
		Content: "Apples: 10\nPears: 20\n",
	}); err != nil {
		log.Fatal(err)
	}

	timeoutMs, maxOutput := 5_000, 10_000
	bash, err := toolbash.CreateBashTool(ctx, toolbash.Options{
		Sandbox:         sandbox,
		TimeoutMs:       &timeoutMs, // each command is killed after 5 seconds
		MaxOutputLength: &maxOutput, // each stream is truncated to 10,000 characters
	})
	if err != nil {
		log.Fatal(err)
	}

	// Pass the tool to a model with GenerateTextOptions{Tools: []types.Tool{bash}}.
	// Here the model's tool call is simulated by calling Execute directly.
	out, err := bash.Execute(ctx, map[string]interface{}{"command": "cat notes.txt | sort > summary.txt; cat summary.txt"}, types.ToolExecutionOptions{ToolCallID: "call-1"})
	if err != nil {
		log.Fatal(err)
	}
	result := out.(toolbash.Output)
	fmt.Printf("exit code: %d\nstdout:\n%s", result.ExitCode, result.Stdout)
}

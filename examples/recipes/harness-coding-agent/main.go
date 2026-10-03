// Recipe: run Claude Code (or Codex) in a sandbox and read its changes back.
//
// Run: ANTHROPIC_API_KEY=... go run ./examples/recipes/harness-coding-agent
// Needs Node.js on the machine: the local sandbox runs the harness bridge with it.
// The local sandbox is NOT isolated. It runs commands as your user.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
	"github.com/digitallysavvy/go-ai/pkg/harness/sandbox/local"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

const brokenFile = `package main

import "fmt"

func add(a, b int) int { return a - b } // bug

func main() { fmt.Println(add(2, 3)) }
`

func main() {
	ctx := context.Background()

	cc, err := claudecode.New(claudecode.Settings{MaxTurns: 20})
	if err != nil {
		log.Fatal(err)
	}

	coder, err := harness.NewAgent(harness.AgentSettings{
		Harness:        cc,
		Model:          anthropic.ClaudeSonnet5_5,
		PermissionMode: harness.PermissionModeAllowAll,
		Instructions:   "Fix the bug with a minimal change. End with one sentence describing it.",
		// The local provider needs Ports: the harness bridge listens on one.
		Sandbox: local.NewProvider(local.Options{
			RootDir: filepath.Join(os.TempDir(), "recipe-sandboxes"),
			Ports:   []int{4319},
		}),
		// Runs once the sandbox exists, before the agent starts. Seed files here.
		SandboxConfig: harness.AgentSandboxConfig{
			OnSession: func(ctx context.Context, sc harness.SandboxSessionContext) error {
				return sc.Session.WriteTextFile(ctx, providerutils.SandboxWriteTextFileOptions{
					Path:    path.Join(sc.SessionWorkDir, "main.go"),
					Content: brokenFile,
				})
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	session, err := coder.CreateSession(ctx, harness.CreateSessionOptions{SessionID: "recipe-session"})
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = session.Destroy(context.WithoutCancel(ctx)) }()

	result, err := coder.Stream(ctx, agent.AgentStreamOptions{AgentGenerateOptions: agent.AgentGenerateOptions{
		HarnessSession: session,
		Prompt:         "add(2, 3) in main.go returns -1 but should return 5. Fix it.",
	}})
	if err != nil {
		log.Fatal(err)
	}

	// Print the agent's activity as it happens.
	stream := result.Stream()
	for {
		chunk, err := stream.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		switch chunk.Type {
		case provider.ChunkTypeText:
			fmt.Print(chunk.Text)
		case provider.ChunkTypeToolCall:
			if chunk.ToolCall != nil {
				fmt.Printf("\n[tool] %s\n", chunk.ToolCall.ToolName)
			}
		}
	}
	if err := stream.Err(); err != nil {
		log.Fatal(err)
	}

	// Read the result back through the sandbox API.
	content, err := session.GetSandboxSession().ReadTextFile(ctx, providerutils.SandboxReadTextFileOptions{
		Path: path.Join(session.GetSessionWorkDir(), "main.go"),
	})
	if err != nil {
		log.Fatal(err)
	}
	if content != nil {
		fmt.Println("\n--- main.go ---")
		fmt.Println(*content)
	}
}

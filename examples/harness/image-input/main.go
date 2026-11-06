//go:build ignore

// This example sends an inline image to a coding-agent harness. The image is
// read from the file named by the first argument and passed as bytes next to a
// text part. Claude Code, Codex, OpenCode, Deep Agents and the ACP-based
// adapters accept inline images this way; remote image URLs are rejected with a
// *harness.CapabilityUnsupportedError.
//
// Run with: HARNESS_E2E=1 go run examples/harness/image-input/main.go ./cat.png
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
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func main() {
	if os.Getenv("HARNESS_E2E") != "1" || len(os.Args) < 2 {
		fmt.Println("Set HARNESS_E2E=1 (and ANTHROPIC_API_KEY) and pass an image path to run this example.")
		return
	}
	image, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatalf("read image: %v", err)
	}

	cc, err := claudecode.New(claudecode.Settings{MaxTurns: 4})
	if err != nil {
		log.Fatalf("claudecode.New: %v", err)
	}
	agent, err := harness.NewAgent(harness.AgentSettings{
		Harness:        cc,
		Model:          "claude-sonnet-4-5",
		Sandbox:        local.NewProvider(local.Options{Ports: []int{4318}}),
		PermissionMode: harness.PermissionModeAllowAll,
	})
	if err != nil {
		log.Fatalf("harness.NewAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result, err := agent.ExecuteWithMessages(ctx, []types.Message{{
		Role: types.RoleUser,
		Content: []types.ContentPart{
			types.TextContent{Text: "What is in this image?"},
			// The media type may be "image" when the bytes are recognizable.
			types.FileContent{MediaType: "image/png", Data: image},
		},
	}})
	if err != nil {
		log.Fatalf("ExecuteWithMessages: %v", err)
	}
	fmt.Println(result.Text)
}

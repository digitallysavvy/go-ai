//go:build ignore
// +build ignore

// This example builds a live harness.Harness for the Cursor CLI harness
// adapter via cursor.CreateCursor (the real constructor, wired through
// pkg/harness/acp) and prints the ACP recipe pkg/harness/cursor assembles.
// Building the harness and inspecting its metadata requires no network
// access, API key or installed CLI.
//
// Driving an actual session (HARNESS_E2E=1) additionally requires Node.js
// and pnpm on PATH (the sandbox installs the Cursor `agent` CLI on first
// run) and a way for the sandboxed `agent` CLI to authenticate -- either
// CURSOR_API_KEY in the environment or a prior `cursor-agent login`.
//
// Run with: go run ./examples/harness/cursor_config.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
	"github.com/digitallysavvy/go-ai/pkg/harness/cursor"
	"github.com/digitallysavvy/go-ai/pkg/harness/sandbox/local"
)

func main() {
	settings := cursor.Settings{
		MCPServers: map[string]any{
			"weather": map[string]any{"command": "weather-mcp-server"},
		},
	}

	h, err := cursor.CreateCursor(settings)
	if err != nil {
		log.Fatalf("cursor.CreateCursor: %v", err)
	}

	fmt.Printf("harnessId:  %s\n", h.HarnessID())
	fmt.Printf("builtin tool count: %d\n", len(h.BuiltinTools()))

	// BuildConfig is the same acp.Settings recipe CreateCursor wires into
	// acp.CreateACP; inspecting it directly is useful for debugging the
	// exact ACP configuration without a live session.
	cfg := cursor.BuildConfig(settings)
	fmt.Printf("executable: %s %v\n", cfg.Executable, cfg.Args)
	fmt.Printf("source:     %+v\n", cfg.Source)

	// The MCP-tool-call classifier and credential-brokering rules are real,
	// callable Go functions -- no ACP host is needed to exercise them.
	isMCP := cursor.IsMCPToolCall(acp.ToolCall{RawInput: map[string]any{
		"providerIdentifier": "ai-sdk-harness-tools",
		"toolName":           "weather",
		"args":               map[string]any{"city": "Lima"},
	}})
	fmt.Printf("classifies a host-tool MCP call as MCP: %v\n", isMCP)

	transforms := cursor.CredentialBrokering(cfg.Auth,
		map[string]string{"CURSOR_API_KEY": "host-secret"},
		map[string]string{"CURSOR_API_KEY": "sandbox-placeholder"},
		nil,
	)
	encoded, _ := json.MarshalIndent(transforms, "", "  ")
	fmt.Printf("credential-brokering transformations:\n%s\n", encoded)

	if os.Getenv("HARNESS_E2E") != "1" {
		fmt.Println("Set HARNESS_E2E=1 (with Node/pnpm on PATH and Cursor CLI credentials) to run a live session.")
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
	fmt.Println("=== Cursor response ===")
	fmt.Println(result.Text)
}

//go:build ignore
// +build ignore

// This example prints the ACP recipe pkg/harness/cursor assembles for the
// Cursor CLI harness adapter. It requires no network access, API key or
// installed CLI: it only exercises the Go-native configuration layer.
//
// pkg/harness/cursor.BuildConfig is NOT yet wired to a live harness.Harness:
// that requires pkg/harness/acp (the ACP meta-adapter host, WG11), which had
// not landed when this adapter was ported (see the pkg/harness/cursor
// package doc). Once it lands, running a real session against the sandboxed
// `agent` CLI is a matter of calling
//
//	h, err := acp.CreateACP(cfg) // cfg is a cursor.Config built below
//
// and driving h like any other harness.Harness (see harness.md for the
// runtime model).
//
// Run with: go run ./examples/harness/cursor_config.go
package main

import (
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/harness/cursor"
)

func main() {
	cfg := cursor.BuildConfig(cursor.Settings{
		MCPServers: map[string]any{
			"weather": map[string]any{"command": "weather-mcp-server"},
		},
	})

	fmt.Printf("harnessId:  %s\n", cfg.HarnessID)
	fmt.Printf("executable: %s %v\n", cfg.Executable, cfg.Args)
	fmt.Printf("source:     %+v\n", cfg.Source)
	fmt.Printf("builtin tool count: %d\n", len(cfg.BuiltinTools))

	// The MCP-tool-call classifier and credential-brokering rules are real,
	// callable Go functions -- no ACP host is needed to exercise them.
	isMCP := cursor.IsMCPToolCall(cursor.ToolCall{RawInput: map[string]any{
		"providerIdentifier": "ai-sdk-harness-tools",
		"toolName":           "weather",
		"args":               map[string]any{"city": "Lima"},
	}})
	fmt.Printf("classifies a host-tool MCP call as MCP: %v\n", isMCP)

	transforms, err := cursor.CredentialBrokering(cfg.Auth,
		map[string]string{"CURSOR_API_KEY": "host-secret"},
		map[string]string{"CURSOR_API_KEY": "sandbox-placeholder"},
		nil,
	)
	if err != nil {
		panic(err)
	}
	encoded, _ := json.MarshalIndent(transforms, "", "  ")
	fmt.Printf("credential-brokering transformations:\n%s\n", encoded)
}

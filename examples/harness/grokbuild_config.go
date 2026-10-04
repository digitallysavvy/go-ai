//go:build ignore
// +build ignore

// This example builds a live harness.Harness for the Grok Build CLI harness
// adapter via grokbuild.CreateGrokBuild (the real constructor, wired through
// pkg/harness/acp), prints the ACP recipe pkg/harness/grokbuild assembles,
// and drives its askUserQuestions native-protocol translation end to end
// (no CLI, network or ACP host needed for either).
//
// Driving an actual session (HARNESS_E2E=1) additionally requires Node.js
// and pnpm on PATH (the sandbox installs the `grok` CLI on first run) and a
// way for the sandboxed CLI to authenticate (XAI_API_KEY, or a prior
// `grok login`).
//
// Run with: go run ./examples/harness/grokbuild_config.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/grokbuild"
	"github.com/digitallysavvy/go-ai/pkg/harness/sandbox/local"
)

func main() {
	settings := grokbuild.Settings{ReasoningEffort: grokbuild.ReasoningEffortHigh}

	h, err := grokbuild.CreateGrokBuild(settings)
	if err != nil {
		log.Fatalf("grokbuild.CreateGrokBuild: %v", err)
	}

	fmt.Printf("harnessId:  %s\n", h.HarnessID())
	fmt.Printf("builtin tool count: %d\n", len(h.BuiltinTools()))

	cfg, err := grokbuild.BuildConfig(settings)
	if err != nil {
		log.Fatalf("grokbuild.BuildConfig: %v", err)
	}
	fmt.Printf("executable: %s %v\n", cfg.Executable, cfg.Args)
	fmt.Printf("outputSchemaMapping: %+v\n", cfg.OutputSchemaMapping)

	var nativeRequest any
	if err := json.Unmarshal([]byte(`{
		"sessionId": "session-1",
		"toolCallId": "call-1",
		"mode": "default",
		"questions": [
			{
				"question": "Which package manager?",
				"options": [
					{"label": "pnpm", "description": "Fast, disk-efficient"},
					{"label": "npm", "description": "Comes with Node"}
				]
			}
		]
	}`), &nativeRequest); err != nil {
		panic(err)
	}
	part := grokbuild.AskUserQuestions.FromNativeRequest(nativeRequest, nil)
	if part == nil {
		panic("expected a translated tool-call part")
	}
	fmt.Printf("translated tool-call: toolName=%s nativeName=%s\n", part.ToolName, part.NativeName)
	fmt.Printf("harness-v1 input JSON: %s\n", part.Input)

	if os.Getenv("HARNESS_E2E") != "1" {
		fmt.Println("Set HARNESS_E2E=1 (with Node/pnpm on PATH and Grok Build CLI credentials) to run a live session.")
		return
	}

	agent, err := harness.NewAgent(harness.AgentSettings{
		Harness:        h,
		Model:          "auto",
		Sandbox:        local.NewProvider(local.Options{Ports: []int{4318}}), // the harness bridge listens on this port
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
	fmt.Println("=== Grok Build response ===")
	fmt.Println(result.Text)
}

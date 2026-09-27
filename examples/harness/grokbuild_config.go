//go:build ignore
// +build ignore

// This example prints the ACP recipe pkg/harness/grokbuild assembles for
// the Grok Build CLI harness adapter, and drives its askUserQuestions
// native-protocol translation end to end (no CLI, network or ACP host
// needed for either).
//
// See pkg/harness/cursor's package doc for why this stops at BuildConfig
// instead of a live harness.Harness (pending pkg/harness/acp, WG11).
//
// Run with: go run ./examples/harness/grokbuild_config.go
package main

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/harness/grokbuild"
)

func main() {
	cfg, err := grokbuild.BuildConfig(grokbuild.Settings{
		ReasoningEffort: grokbuild.ReasoningEffortHigh,
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("harnessId:  %s\n", cfg.HarnessID)
	fmt.Printf("executable: %s %v\n", cfg.Executable, cfg.Args)
	fmt.Printf("outputSchemaMapping: %+v\n", cfg.OutputSchemaMapping)
	fmt.Printf("builtin tool count: %d\n", len(cfg.BuiltinTools))

	nativeRequest := []byte(`{
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
	}`)
	part, err := grokbuild.AskUserQuestions.FromNativeRequest(nativeRequest)
	if err != nil {
		panic(err)
	}
	fmt.Printf("translated tool-call: toolName=%s nativeName=%s\n", part.ToolName, part.NativeName)
	fmt.Printf("harness-v1 input JSON: %s\n", part.Input)
}

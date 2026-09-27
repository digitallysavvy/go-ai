//go:build ignore

// This example drives the Claude Code coding-agent runtime through
// pkg/workflow's workflow-harness helpers (pkg/workflow/harness.go),
// simulating the durable step loop a Vercel Workflow DevKit `'use step'`
// function would run: each RunHarnessAgentTimeSlice call is one execution,
// and its returned HarnessWorkflowState is exactly what a real workflow step
// would persist as its return value and hand back on the next invocation.
// This example just loops locally instead of actually suspending between
// executions.
//
// Uses the local sandbox provider (pkg/harness/sandbox/local) so no Vercel
// Sandbox account is required.
//
// Requirements:
//   - Node.js >= 20 and pnpm on PATH (the sandbox installs the embedded
//     bridge's dependencies, including the `claude` CLI, on first run).
//   - ANTHROPIC_API_KEY (or AI_GATEWAY_API_KEY / CLAUDE_CODE_OAUTH_TOKEN) set
//     in the environment.
//
// Run with: go run examples/workflow/harness/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
	"github.com/digitallysavvy/go-ai/pkg/harness/sandbox/local"
	"github.com/digitallysavvy/go-ai/pkg/workflow"
)

// stdoutWriter prints each UI-message chunk's type as it arrives — the
// simplest possible HarnessWorkflowWriter. A real workflow step would write
// to the run's actual output stream instead (e.g. an SSE response, or
// getWritable()'s Go equivalent once one exists).
type stdoutWriter struct{}

func (stdoutWriter) Write(chunk ai.UIMessageChunk) error {
	fmt.Printf("  chunk: %v\n", chunk["type"])
	return nil
}
func (stdoutWriter) Close() error {
	fmt.Println("  (writable closed)")
	return nil
}

func main() {
	if os.Getenv("HARNESS_E2E") != "1" {
		fmt.Println("Set HARNESS_E2E=1 (and ANTHROPIC_API_KEY, with Node/pnpm on PATH) to run this example.")
		return
	}

	cc, err := claudecode.New(claudecode.Settings{MaxTurns: 8})
	if err != nil {
		log.Fatalf("claudecode.New: %v", err)
	}

	agent, err := harness.NewAgent(harness.AgentSettings{
		Harness:        cc,
		Model:          "claude-sonnet-4-5",
		Sandbox:        local.NewProvider(local.Options{}),
		PermissionMode: harness.PermissionModeAllowAll,
		// A StopWhen of one step per execution lets this example show
		// multiple time slices even for a fast-finishing prompt. Omit this
		// in a real deployment; TimeSliceSeconds alone is usually enough.
		StopWhen: []ai.StopCondition{ai.IsStepCount(1)},
	})
	if err != nil {
		log.Fatalf("harness.NewAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	state := workflow.CreateHarnessWorkflowState(workflow.HarnessWorkflowInput{
		Prompt:    harness.TextPrompt("List the files in the current directory, then say hello."),
		SessionID: "example-session-1",
	})

	fmt.Println("=== Claude Code via workflow-harness ===")
	for {
		next, err := workflow.RunHarnessAgentTimeSlice(ctx, workflow.RunHarnessAgentTimeSliceOptions{
			Agent: agent, State: state, TimeSliceSeconds: 60, Writable: stdoutWriter{},
		})
		if err != nil {
			log.Fatalf("RunHarnessAgentTimeSlice: %v", err)
		}
		state = next
		fmt.Printf("=== execution done: status=%s ===\n", state.Status)

		switch state.Status {
		case workflow.HarnessWorkflowStatusFinished, workflow.HarnessWorkflowStatusFailed, workflow.HarnessWorkflowStatusAwaitingToolApproval:
			result, err := workflow.FinalizeHarnessWorkflow(state)
			if err != nil {
				log.Fatalf("FinalizeHarnessWorkflow: %v", err)
			}
			fmt.Printf("finishReason=%s\n", result.FinishReason)
			return
		case workflow.HarnessWorkflowStatusReadyForNextStep:
			// A real durable-step wrapper would return `state` here and let
			// the next workflow execution pick it back up; this example
			// just loops immediately.
			continue
		}
	}
}

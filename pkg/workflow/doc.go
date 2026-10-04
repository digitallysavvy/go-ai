// Package workflow runs agents as durable workflows: runs that can pause,
// survive a restart, and resume where they stopped. It suits long tasks, tool
// approvals that wait for a person, and chat streams that a browser must be able
// to reconnect to.
//
// The main types are:
//
//   - WorkflowAgent: a tool-loop agent whose steps and state can be serialized.
//     Call Generate, Stream, or the *WithOptions variants.
//   - WorkflowChatTransport and WorkflowRunMultiplexer: stream a run to a chat UI
//     and reconnect to it after a dropped connection.
//   - RunHarnessAgent and its variants: run a harness.Agent turn as one step of
//     a durable workflow, suspending and resuming across steps (a tool-approval
//     pause, a time-slice budget, or a step boundary).
//   - SerializableToolDef, SerializeToolSet and ResolveSerializableTools: turn a
//     tool set into JSON and back.
//
// Create an agent:
//
//	agent, err := workflow.NewWorkflowAgent(workflow.WorkflowAgent{
//		ID:           "support-agent",
//		Model:        model,
//		Instructions: "You are a support assistant.",
//		Tools:        []types.Tool{searchTool},
//		StopWhen:     []ai.StopCondition{ai.IsStepCount(20)},
//	})
//
// Guide: https://goaisdk.com/docs/agents/workflow-agent.
//
// Provenance: WorkflowAgent mirrors @ai-sdk/workflow, and the harness helpers
// in harness.go port @ai-sdk/workflow-harness (ai@7.0.127). The TypeScript
// packages target Vercel's Workflow DevKit, whose 'use step' functions persist
// their return value as the durable checkpoint and expose a process-wide
// writable stream. Go has no equivalent runtime, so
// RunHarnessAgentOptions.Writable is always explicit and you supply your own
// step and durability wrapper around RunHarnessAgent. See
// examples/workflow/harness for the intended shape.
package workflow

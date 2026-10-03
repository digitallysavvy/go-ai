// Package agent builds agents that call a model, run the tools it asks for,
// and repeat until the task is done.
//
// ToolLoopAgent is the main type. Give it a model, a system prompt and a list
// of tools, then call Execute (or Generate and Stream for the full set of call
// options). The agent stops when the model answers without a tool call, when it
// reaches MaxSteps, or when a stop condition you set returns true.
//
//	myAgent := agent.NewToolLoopAgent(agent.AgentConfig{
//		Model:    model,
//		System:   "You are a helpful research assistant.",
//		Tools:    []types.Tool{searchTool},
//		MaxSteps: 10,
//	})
//
//	result, err := myAgent.Execute(ctx, "What is the population of Tokyo?")
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Println(result.Text)
//
// An agent can also delegate work to subagents (AddSubagent), load reusable
// skills (AddSkill), and require approval before a tool runs (set
// types.Tool.ToolApproval).
//
// To serve an agent to a browser chat UI, use CreateAgentUIStreamResponse,
// PipeAgentUIStreamToResponse, or the FromUIMessages variants. They accept
// the message history that the AI SDK useChat hook posts and write the UI
// message stream to an http.ResponseWriter.
//
// Guides: https://goaisdk.com/docs/agents/overview and
// https://goaisdk.com/docs/agents/building-agents.
//
// This package is the Go counterpart of ToolLoopAgent and the agent UI stream
// helpers in the Vercel AI SDK for TypeScript.
package agent

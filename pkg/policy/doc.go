// Package policy connects tool approval and model calls to a policy engine
// such as Open Policy Agent (OPA). A policy decides which tool calls run
// automatically, which need a person to approve them, and which are denied.
//
//   - OPAPolicy returns a tool approval function that asks a PolicyClient for a
//     decision on each tool call. Assign it to types.Tool.ToolApproval.
//   - OptionalOPAPolicy does the same, and returns nil (no policy) when you pass
//     no client.
//   - OPACapabilityMiddleware is language model middleware that asks the policy
//     engine about each model call before it runs.
//   - WrapMCPTools applies an approval policy to tools from an MCP server.
//   - Shadow wraps an approval in audit mode: it evaluates the policy and reports
//     the decision through a callback, but approves the call unless you set
//     Enforce. Use it to test a policy before you enforce it.
//
// Guide: https://goaisdk.com/docs/agents/building-agents.
package policy

// Package tui runs an agent in a terminal. RunAgentTUI reads prompts from
// standard input, streams the agent's answer with Markdown rendering, shows
// response statistics, and asks you to approve tool calls that need approval.
// Use the lower-level AgentTUIRunner to supply your own prompt reader,
// renderer or approval reader, for example in tests.
//
// Guide: https://goaisdk.com/docs/agents/building-agents.
package tui

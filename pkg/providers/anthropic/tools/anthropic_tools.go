// Package tools provides Anthropic-specific tools for computer use, code execution,
// and tool search capabilities.
//
// These tools enable Claude to:
//   - Control computers through mouse, keyboard, and screenshots (Computer Use)
//   - Execute shell commands in persistent bash sessions (Bash)
//   - View and modify text files (Text Editor)
//   - Run Python and bash code in sandboxed environments (Code Execution)
//   - Discover and load tools on-demand from large catalogs (Tool Search)
//
// All tools in this package are executed by the Anthropic API, not locally.
// They must have ProviderExecuted set to true.
package tools

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// AnthropicTools provides access to all Anthropic-specific tools
var AnthropicTools = struct {
	// Computer20251124 creates a computer use tool with mouse, keyboard, and screenshot capabilities.
	// This is the latest version (v20251124) which includes the zoom action.
	// Supported models: Claude Opus 4.5
	Computer20251124 func(Computer20251124Args) types.Tool

	// Bash20250124 creates a bash tool for executing shell commands in a persistent session.
	// Supported models: Claude Opus 4.5, Claude Sonnet 4.5
	Bash20250124 func() types.Tool

	// TextEditor20250728 creates a text editor tool for viewing and modifying files.
	// Supports view, create, str_replace, and insert commands.
	// Supported models: Claude Sonnet 4, Claude Opus 4, Claude Opus 4.1
	TextEditor20250728 func(TextEditor20250728Args) types.Tool

	// CodeExecution20250825 creates a code execution tool for running Python and bash code.
	// Supports programmatic tool calling, bash execution, and text editor operations.
	// Supported models: Claude Opus 4.5, Claude Sonnet 4.5
	CodeExecution20250825 func() types.Tool

	// CodeExecution20260120 creates the client-side code execution tool (version 2026-01-20).
	// Supports Python programmatic tool calls, bash execution, and text editor file operations.
	// The required beta header is automatically injected when this tool is in the tool list.
	// Supported models: Claude Opus 4.6, Claude Sonnet 4.6
	CodeExecution20260120 func() types.Tool

	// ToolSearchBm2520251119 creates a BM25-based tool search for natural language queries.
	// Enables Claude to work with hundreds or thousands of tools by discovering them on-demand.
	// Supported models: Claude Opus 4.5, Claude Sonnet 4.5
	ToolSearchBm2520251119 func() types.Tool

	// ToolSearchRegex20251119 creates a regex-based tool search for pattern matching.
	// Enables Claude to work with hundreds or thousands of tools by discovering them on-demand.
	// Supported models: Claude Opus 4.5, Claude Sonnet 4.5
	ToolSearchRegex20251119 func() types.Tool
}{
	Computer20251124:        Computer20251124,
	Bash20250124:            Bash20250124,
	TextEditor20250728:      TextEditor20250728,
	CodeExecution20250825:   CodeExecution20250825,
	CodeExecution20260120:   CodeExecution20260120,
	ToolSearchBm2520251119:  ToolSearchBm2520251119,
	ToolSearchRegex20251119: ToolSearchRegex20251119,
}

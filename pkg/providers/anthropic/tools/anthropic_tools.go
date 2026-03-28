// Package tools provides Anthropic-specific tools for computer use, code execution,
// tool search, and web search/fetch capabilities.
//
// These tools enable Claude to:
//   - Control computers through mouse, keyboard, and screenshots (Computer Use)
//   - Execute shell commands in persistent bash sessions (Bash)
//   - View and modify text files (Text Editor)
//   - Run Python and bash code in sandboxed environments (Code Execution)
//   - Store and retrieve information across conversations (Memory)
//   - Discover and load tools on-demand from large catalogs (Tool Search)
//   - Search the web for current information (WebSearch)
//   - Fetch and read content from URLs (WebFetch)
//
// All tools in this package are executed by the Anthropic API, not locally.
// They must have ProviderExecuted set to true.
package tools

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// AnthropicTools provides access to all Anthropic-specific tools
var AnthropicTools = struct {
	// Latest versions

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

	// Memory20250818 creates the memory tool for storing and retrieving information across conversations.
	// Supports view, create, str_replace, insert, delete, and rename commands.
	// Requires beta header: context-management-2025-06-27 (injected automatically).
	// Supported models: Claude Sonnet 4.5, Claude Sonnet 4, Claude Opus 4.1, Claude Opus 4
	Memory20250818 func() types.Tool

	// ToolSearchBm2520251119 creates a BM25-based tool search for natural language queries.
	// Enables Claude to work with hundreds or thousands of tools by discovering them on-demand.
	// Supported models: Claude Opus 4.5, Claude Sonnet 4.5
	ToolSearchBm2520251119 func() types.Tool

	// ToolSearchRegex20251119 creates a regex-based tool search for pattern matching.
	// Enables Claude to work with hundreds or thousands of tools by discovering them on-demand.
	// Supported models: Claude Opus 4.5, Claude Sonnet 4.5
	ToolSearchRegex20251119 func() types.Tool

	// WebSearch20260209 creates an Anthropic web search tool (version 2026-02-09).
	// Enables Claude to search the web for current information with configurable domain
	// filters and user location context. Results include EncryptedContent for multi-turn citations.
	WebSearch20260209 func(WebSearch20260209Config) types.Tool

	// WebFetch20260209 creates an Anthropic web fetch tool (version 2026-02-09).
	// Enables Claude to fetch and read content from URLs, returning either base64-encoded
	// PDF or plain text content.
	WebFetch20260209 func(WebFetch20260209Config) types.Tool

	// Legacy versions

	// Computer20241022 creates a computer use tool (version 2024-10-22).
	// Supported models: Claude Sonnet 3.5
	// Requires beta header: computer-use-2024-10-22 (injected automatically).
	Computer20241022 func(Computer20241022Args) types.Tool

	// Computer20250124 creates a computer use tool (version 2025-01-24).
	// Supported models: Claude Sonnet 3.7
	// Requires beta header: computer-use-2025-01-24 (injected automatically).
	Computer20250124 func(Computer20241022Args) types.Tool

	// Bash20241022 creates a bash tool for executing shell commands (version 2024-10-22).
	// Supported models: Claude Sonnet 3.5
	// Requires beta header: computer-use-2024-10-22 (injected automatically).
	Bash20241022 func() types.Tool

	// TextEditor20241022 creates a text editor tool (version 2024-10-22).
	// Supports view, create, str_replace, insert, and undo_edit commands.
	// Supported models: Claude Sonnet 3.5
	// Requires beta header: computer-use-2024-10-22 (injected automatically).
	TextEditor20241022 func() types.Tool

	// TextEditor20250124 creates a text editor tool (version 2025-01-24).
	// Supports view, create, str_replace, insert, and undo_edit commands.
	// Supported models: Claude Sonnet 3.7
	// Requires beta header: computer-use-2025-01-24 (injected automatically).
	TextEditor20250124 func() types.Tool

	// TextEditor20250429 creates a text editor tool (version 2025-04-29).
	// Supports view, create, str_replace, and insert commands. Does NOT support undo_edit.
	// Deprecated: Use TextEditor20250728 instead.
	// Requires beta header: computer-use-2025-01-24 (injected automatically).
	TextEditor20250429 func() types.Tool

	// CodeExecution20250522 creates the code execution tool (version 2025-05-22).
	// Executes Python code in a secure sandboxed environment.
	// Requires beta header: code-execution-2025-05-22 (injected automatically).
	CodeExecution20250522 func() types.Tool

	// WebSearch20250305 creates an Anthropic web search tool (version 2025-03-05).
	// For the latest version, prefer WebSearch20260209.
	WebSearch20250305 func(WebSearch20260209Config) types.Tool

	// WebFetch20250910 creates an Anthropic web fetch tool (version 2025-09-10).
	// For the latest version, prefer WebFetch20260209.
	// Requires beta header: web-fetch-2025-09-10 (injected automatically).
	WebFetch20250910 func(WebFetch20260209Config) types.Tool
}{
	Computer20251124:        Computer20251124,
	Bash20250124:            Bash20250124,
	TextEditor20250728:      TextEditor20250728,
	CodeExecution20250825:   CodeExecution20250825,
	CodeExecution20260120:   CodeExecution20260120,
	Memory20250818:          Memory20250818,
	ToolSearchBm2520251119:  ToolSearchBm2520251119,
	ToolSearchRegex20251119: ToolSearchRegex20251119,
	WebSearch20260209:       WebSearch20260209,
	WebFetch20260209:        WebFetch20260209,
	Computer20241022:        Computer20241022,
	Computer20250124:        Computer20250124,
	Bash20241022:            Bash20241022,
	TextEditor20241022:      TextEditor20241022,
	TextEditor20250124:      TextEditor20250124,
	TextEditor20250429:      TextEditor20250429,
	CodeExecution20250522:   CodeExecution20250522,
	WebSearch20250305:       WebSearch20250305,
	WebFetch20250910:        WebFetch20250910,
}

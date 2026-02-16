# Anthropic Computer Use Tools

This package provides Anthropic-specific tools for advanced agent capabilities including computer control, code execution, and tool search.

> **💡 Tip:** For custom tools (non-provider tools), you can enable prompt caching to reduce costs by up to 90%. See [Tool Caching](../README.md#tool-caching-prompt-caching) for details.

## Tools Overview

### 1. Computer Use (`computer_20251124`)
Control computers through mouse, keyboard, and screenshot capabilities.

**Actions:**
- Mouse: move, click, drag, scroll
- Keyboard: type, key press, hold key
- Screen: screenshot, zoom, cursor position
- Utility: wait

**Supported Models:** Claude Opus 4.5

**Example:**
```go
import "github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"

computerTool := tools.Computer20251124(tools.Computer20251124Args{
    DisplayWidthPx:  1920,
    DisplayHeightPx: 1080,
    EnableZoom:      true,
})
```

### 2. Bash (`bash_20250124`)
Execute shell commands in a persistent bash session.

**Capabilities:**
- Shell command execution
- Persistent session state
- Session restart

**Supported Models:** Claude Opus 4.5, Claude Sonnet 4.5

**Example:**
```go
bashTool := tools.Bash20250124()
```

### 3. Text Editor (`text_editor_20250728`)
View and modify text files with editor commands.

**Commands:**
- `view`: View file contents or directory
- `create`: Create new file
- `str_replace`: Replace strings
- `insert`: Insert text after line

**Supported Models:** Claude Sonnet 4, Claude Opus 4, Claude Opus 4.1

**Example:**
```go
maxChars := 100000
editorTool := tools.TextEditor20250728(tools.TextEditor20250728Args{
    MaxCharacters: &maxChars,
})
```

### 4. Code Execution (`code_execution_20250825`)
Run Python and bash code in a secure, sandboxed environment.

**Input Types:**
1. **Programmatic Tool Calling**: Execute Python code
2. **Bash Code Execution**: Run shell commands
3. **Text Editor Code Execution**: File operations

**Capabilities:**
- Data analysis and visualizations
- Package installation
- File creation and editing
- Deferred results for programmatic tool calling

**Supported Models:** Claude Opus 4.5, Claude Sonnet 4.5

**Example:**
```go
codeExecTool := tools.CodeExecution20250825()
```

### 5. Tool Search BM25 (`tool_search_bm25_20251119`)
Discover tools using natural language queries with BM25 algorithm.

**Use Cases:**
- Work with hundreds/thousands of tools
- Natural language tool discovery
- On-demand tool loading

**Supported Models:** Claude Opus 4.5, Claude Sonnet 4.5

**Example:**
```go
searchTool := tools.ToolSearchBm2520251119()
// Claude can query: "tools for weather data"
```

### 6. Tool Search Regex (`tool_search_regex_20251119`)
Discover tools using regex patterns (Python re.search() syntax).

**Use Cases:**
- Structured tool naming patterns
- Precise pattern matching
- Programmatic tool discovery

**Supported Models:** Claude Opus 4.5, Claude Sonnet 4.5

**Example:**
```go
searchTool := tools.ToolSearchRegex20251119()
// Claude can use patterns: "get_.*_data", "(?i)slack"
```

## Important Notes

### Provider Execution
All tools in this package are executed by the Anthropic API, not locally:
- `ProviderExecuted: true` is set on all tools
- Tool implementations return errors if called locally
- Actual execution happens server-side at Anthropic

### Tool Search and Deferred Loading
When using tool search:
1. Tool search tools must NOT have `deferLoading: true`
2. Other tools should be marked for deferred loading
3. API automatically expands tool references
4. Dramatically reduces context usage

### Beta Features
Some tools may require beta headers:
- Computer use tools
- Code execution features

Check Anthropic documentation for current beta requirements.

## Examples

See the `/examples/providers/anthropic/` directory for complete working examples:
- `computer-use/` - Computer control demonstrations
- `code-execution/` - Python and bash execution
- `tool-search/` - BM25 and regex tool discovery

## Testing

Run tests:
```bash
cd pkg/providers/anthropic/tools
go test -v
```

## Documentation

- [Anthropic Computer Use Docs](https://docs.anthropic.com/en/docs/agents-and-tools/computer-use)
- [Anthropic Code Execution Docs](https://docs.anthropic.com/en/docs/agents-and-tools/code-execution)
- [Anthropic Tool Search Docs](https://docs.anthropic.com/en/docs/agents-and-tools/tool-search-tool)

## Version History

- `computer_20251124`: Added zoom action
- `bash_20250124`: Latest bash version
- `text_editor_20250728`: Removed undo_edit, added maxCharacters
- `code_execution_20250825`: Enhanced bash and file operations
- `tool_search_*_20251119`: Initial tool search support

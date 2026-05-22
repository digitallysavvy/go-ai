# OpenAI Responses API: Custom Tools & Shell Container Tools

This guide covers the Custom Tool, Tool Search, and Shell Container Tool types available in the OpenAI Responses API, and how to use them with the Go-AI SDK.

## Overview

The OpenAI Responses API supports several tool types beyond standard function calling:

| Tool Type     | Wire Type       | Package                                      |
|--------------|-----------------|----------------------------------------------|
| Function     | `function`      | built-in (`types.Tool`)                      |
| Custom       | `custom`        | `pkg/providers/openai/tool`                  |
| Tool Search  | `tool_search`   | `pkg/providers/openai/tool`                  |
| Local Shell  | `local_shell`   | `pkg/providers/openai/responses`             |
| Shell        | `shell`         | `pkg/providers/openai/responses`             |
| Apply Patch  | `apply_patch`   | `pkg/providers/openai/responses`             |

Use `responses.PrepareTools()` to convert any combination of these into the wire format required by the Responses API.

---

## Custom Tools

A custom tool constrains the model's output to a specific format — either free-form text or a grammar (regex or Lark).

### Package

```go
import openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
```

### Types

```go
type CustomToolFormat struct {
    Type       string  // "grammar" or "text"
    Syntax     *string // "regex" or "lark" (grammar type only)
    Definition *string // the grammar or regex string (grammar type only)
}

// CustomTool does not store a Name field — the name is supplied to ToTool("name").
type CustomTool struct {
    Description *string
    Format      *CustomToolFormat
}
```

### Factory Function

```go
func NewCustomTool(opts ...CustomToolOption) CustomTool
```

Available options:

```go
openaitool.WithDescription(desc string) CustomToolOption
openaitool.WithFormat(format CustomToolFormat) CustomToolOption
```

The tool name is **not** stored in `CustomTool`. Supply it when calling `ToTool("name")` so
the name is derived from the caller's context (e.g., the key in a tools map), matching the
TypeScript SDK convention.

### Usage

```go
// Custom tool with Lark grammar
syntax := "lark"
definition := `start: OBJECT`

ct := openaitool.NewCustomTool(
    openaitool.WithDescription("Extract JSON from the provided text"),
    openaitool.WithFormat(openaitool.CustomToolFormat{
        Type:       "grammar",
        Syntax:     &syntax,
        Definition: &definition,
    }),
)

// Convert to types.Tool — supply the name here
tool := ct.ToTool("json-extractor")
```

```go
// Custom tool with regex grammar
syntax := "regex"
definition := `^\d{4}-\d{2}-\d{2}$`

ct := openaitool.NewCustomTool(
    openaitool.WithDescription("Extract a date in YYYY-MM-DD format"),
    openaitool.WithFormat(openaitool.CustomToolFormat{
        Type:       "grammar",
        Syntax:     &syntax,
        Definition: &definition,
    }),
)
tool := ct.ToTool("date-extractor")
```

```go
// Custom tool with text format (unconstrained output)
ct := openaitool.NewCustomTool(
    openaitool.WithDescription("Analyze the sentiment of the provided text"),
    openaitool.WithFormat(openaitool.CustomToolFormat{Type: "text"}),
)
tool := ct.ToTool("sentiment-analyzer")
```

### Wire Format

When `ct.ToTool("name")` is passed through `responses.PrepareTools()`, it serializes to:

```json
{
  "type": "custom",
  "name": "json-extractor",
  "description": "Extract JSON from the provided text",
  "format": {
    "type": "grammar",
    "syntax": "lark",
    "definition": "start: OBJECT"
  }
}
```

---

## Tool Search

The tool_search tool enables the model to search across deferred tools. There are two execution modes:

- **Server mode** (default): OpenAI resolves tool matches internally. No `tool_search_call` event is emitted.
- **Client mode**: The model emits a `tool_search_call` event. The client's `Execute` function is called with search arguments and should return matching tool names.

### Factory Function

```go
type ToolSearchArgs struct {
    Execution   string  // "server" (default) or "client"
    Description string  // describes the search capability (client mode)
    Parameters  map[string]interface{} // JSON schema for search arguments (client mode)
    Execute     func(ctx, input, opts) (interface{}, error) // called in client mode
}

func ToolSearch(args ToolSearchArgs) types.Tool
```

### Usage

```go
// Server mode (default): OpenAI handles the search
searchTool := openaitool.ToolSearch(openaitool.ToolSearchArgs{})
```

```go
// Client mode: route tool_search_call events to your Execute function
searchTool := openaitool.ToolSearch(openaitool.ToolSearchArgs{
    Execution:   "client",
    Description: "Find tools matching a query",
    Parameters: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "query": map[string]interface{}{"type": "string"},
        },
        "required": []string{"query"},
    },
    Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
        query, _ := input["query"].(string)
        // Return tool names matching the query
        return []string{"get_weather", "search_web"}, nil
    },
})
```

### Wire Format

Server mode serializes to:
```json
{"type": "tool_search"}
```

Client mode serializes to:
```json
{
  "type": "tool_search",
  "execution": "client",
  "description": "Find tools matching a query",
  "parameters": {"type": "object", "properties": {"query": {"type": "string"}}}
}
```

---

## Shell Container Tools

Shell tools allow the model to interact with a sandboxed environment. There are three variants:

### Package

```go
import "github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
```

### Local Shell Tool

Executes commands in a local (sandboxed) shell. No container configuration needed.

```go
tool := responses.NewLocalShellTool()
```

Wire format:

```json
{"type": "local_shell"}
```

### Shell Tool

Runs commands in a managed container environment. Supports auto-provisioned and referenced containers.

```go
// Shell tool with auto-provisioned container
memLimit := "2g"
tool := responses.NewShellTool(
    responses.WithShellEnvironment(responses.ShellEnvironment{
        Type:        "container_auto",
        MemoryLimit: &memLimit,
        FileIDs:     []string{"file-abc123"}, // files to mount
        NetworkPolicy: &responses.ShellNetworkPolicy{
            Type:           "allowlist",
            AllowedDomains: []string{"pypi.org"},
        },
    }),
)
```

```go
// Shell tool referencing an existing container
containerID := "cntr_abc123"
tool := responses.NewShellTool(
    responses.WithShellEnvironment(responses.ShellEnvironment{
        Type:        "container_reference",
        ContainerID: &containerID,
    }),
)
```

```go
// Shell tool with no environment (uses default)
tool := responses.NewShellTool()
```

#### ShellEnvironment Fields

| Field           | Type                  | Description                                        |
|----------------|-----------------------|----------------------------------------------------|
| `Type`         | `string`              | `"container_auto"`, `"container_reference"`, `"local"` |
| `FileIDs`      | `[]string`            | Files to mount (container_auto only)               |
| `MemoryLimit`  | `*string`             | Memory limit e.g. `"2g"` (container_auto only)     |
| `NetworkPolicy`| `*ShellNetworkPolicy` | Network access control (container_auto only)       |
| `Skills`       | `[]ShellSkill`        | Executable skills in the container                 |
| `ContainerID`  | `*string`             | Existing container ID (container_reference only)   |

#### ShellNetworkPolicy

```go
type ShellNetworkPolicy struct {
    Type           string             // "allowlist" or "none"
    AllowedDomains []string           // allowed hostnames
    DomainSecrets  []ShellDomainSecret
}
```

### Apply Patch Tool

Enables the model to create, update, or delete files using unified diffs.

```go
tool := responses.NewApplyPatchTool()
```

Wire format:

```json
{"type": "apply_patch"}
```

---

## PrepareTools

`PrepareTools` converts a `[]types.Tool` slice into the `[]interface{}` slice expected as the `"tools"` field in a Responses API request body.
Function tools with nil or implicit object parameters serialize with an
explicit `{"type":"object","properties":{}}` schema when needed, matching the
TypeScript SDK request shape.

```go
import (
    "github.com/digitallysavvy/go-ai/pkg/provider/types"
    "github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
    openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

memLimit := "4g"
tools := responses.PrepareTools([]types.Tool{
    // Standard function tool
    {Name: "get_weather", Description: "Get weather"},
    // Custom tool — name supplied to ToTool("name")
    openaitool.NewCustomTool().ToTool("json-extractor"),
    // Shell tools
    responses.NewLocalShellTool(),
    responses.NewShellTool(responses.WithShellEnvironment(responses.ShellEnvironment{
        Type:        "container_auto",
        MemoryLimit: &memLimit,
    })),
    responses.NewApplyPatchTool(),
})

// tools is ready to be marshaled into a Responses API request
```

### Restricting Callable Tools

Use the OpenAI Responses `allowedTools` provider option when you want to keep
the full tools list in the request for prompt caching, but restrict which
function tools may be called in this step. This overrides request-level
`ToolChoice` and serializes `tool_choice` as `allowed_tools`.

```go
result, err := model.DoGenerate(ctx, &provider.GenerateOptions{
    Prompt: prompt,
    Tools: []types.Tool{
        {Name: "weather", Description: "Get weather"},
        {Name: "cityAttractions", Description: "Find city attractions"},
    },
    ProviderOptions: map[string]interface{}{
        "openai": map[string]interface{}{
            "allowedTools": map[string]interface{}{
                "toolNames": []string{"weather"},
                // Optional: "auto" (default) or "required".
                "mode": "auto",
            },
        },
    },
})
```

Wire shape:

```json
{
  "tools": [
    {"type": "function", "name": "weather"},
    {"type": "function", "name": "cityAttractions"}
  ],
  "tool_choice": {
    "type": "allowed_tools",
    "mode": "auto",
    "tools": [{"type": "function", "name": "weather"}]
  }
}
```

---

## Response Types

When the model uses shell tools, the Responses API returns typed output items. These types are defined in the `responses` package:

### LocalShellCallOutput

```go
type LocalShellCallOutput struct {
    Type   string // "local_shell_call_output"
    CallID string
    Output string // combined stdout/stderr
}
```

### ShellCallOutput

```go
type ShellCallOutput struct {
    Type   string
    CallID string
    Status *string
    Output []ShellCallOutputEntry
}

type ShellCallOutputEntry struct {
    Stdout  string
    Stderr  string
    Outcome ShellOutcome
}

type ShellOutcome struct {
    Type     string // "exit_code" or "timeout"
    ExitCode *int
}
```

### ApplyPatchCallOutput

```go
type ApplyPatchCallOutput struct {
    Type   string
    CallID string
    Status string // "completed" or "failed"
    Output *string
}
```

### AssistantMessageItem with Phase

The Responses API may include a `phase` field on assistant message items to indicate the agentic flow phase:

```go
type AssistantMessageItem struct {
    Type    string
    Role    string
    ID      string
    Phase   *string // "commentary", "final_answer", or nil
    Content []AssistantMessageContent
}
```

Values:
- `"commentary"` — intermediate reasoning or commentary from the model
- `"final_answer"` — the model's conclusive response
- `nil` — phase not specified (standard non-agentic response)

---

## Examples

Full runnable examples are in:

- `examples/providers/openai/responses/custom-tool-grammar/` — Custom tool grammar formats
- `examples/providers/openai/responses/shell-tool/` — Shell container tool configurations
- `examples/providers/openai/tool-search/` — Tool search (server and client modes)

---

## Server-Side Compaction

When the Responses API compacts the conversation context server-side, it emits a
`compaction` event in the SSE stream. The Go-AI SDK surfaces this as a
`ChunkTypeCustom` stream chunk with `CustomContent{Kind: "openai-compaction"}`.

The `ProviderMetadata` JSON on the chunk contains:

| Field              | Description                                          |
|--------------------|------------------------------------------------------|
| `type`             | Always `"compaction"`                                |
| `itemId`           | The item ID of the compacted item                    |
| `encryptedContent` | Opaque encrypted context blob (forward in next turn) |

### Converting a compaction event

```go
import "github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"

// In your streaming parser, when you receive a compaction event:
event := responses.CompactionEvent{
    Type:             "compaction",
    ItemID:           "item_abc",
    EncryptedContent: "enc_xyz...",
}
chunk := responses.CompactionEventToChunk(event)
// chunk.Type == provider.ChunkTypeCustom
// chunk.CustomContent.Kind == "openai-compaction"
```

When you pass that custom content back in a later assistant message, the
Responses prompt converter forwards it as a `compaction` item. If `store` is
enabled and the item has an OpenAI item ID, the converter sends an
`item_reference` instead, matching the TypeScript SDK replay behavior.

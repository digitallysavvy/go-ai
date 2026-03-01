# OpenAI Responses API: Custom Tools & Shell Container Tools

This guide covers the Custom Tool and Shell Container Tool types available in the OpenAI Responses API, and how to use them with the Go-AI SDK.

## Overview

The OpenAI Responses API supports several tool types beyond standard function calling:

| Tool Type     | Wire Type       | Package                                      |
|--------------|-----------------|----------------------------------------------|
| Function     | `function`      | built-in (`types.Tool`)                      |
| Custom       | `custom`        | `pkg/providers/openai/tool`                  |
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

type CustomTool struct {
    Name        string
    Description *string
    Format      *CustomToolFormat
}
```

### Factory Function

```go
func NewCustomTool(name string, opts ...CustomToolOption) CustomTool
```

Available options:

```go
openaitool.WithDescription(desc string) CustomToolOption
openaitool.WithFormat(format CustomToolFormat) CustomToolOption
```

### Usage

```go
// Custom tool with Lark grammar
syntax := "lark"
definition := `start: OBJECT`

ct := openaitool.NewCustomTool("json-extractor",
    openaitool.WithDescription("Extract JSON from the provided text"),
    openaitool.WithFormat(openaitool.CustomToolFormat{
        Type:       "grammar",
        Syntax:     &syntax,
        Definition: &definition,
    }),
)

// Convert to types.Tool for use with PrepareTools or ai.GenerateText
tool := ct.ToTool()
```

```go
// Custom tool with regex grammar
syntax := "regex"
definition := `^\d{4}-\d{2}-\d{2}$`

ct := openaitool.NewCustomTool("date-extractor",
    openaitool.WithDescription("Extract a date in YYYY-MM-DD format"),
    openaitool.WithFormat(openaitool.CustomToolFormat{
        Type:       "grammar",
        Syntax:     &syntax,
        Definition: &definition,
    }),
)
```

```go
// Custom tool with text format (unconstrained output)
ct := openaitool.NewCustomTool("sentiment-analyzer",
    openaitool.WithDescription("Analyze the sentiment of the provided text"),
    openaitool.WithFormat(openaitool.CustomToolFormat{Type: "text"}),
)
```

### Wire Format

When `ct.ToTool()` is passed through `responses.PrepareTools()`, it serializes to:

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
    // Custom tool
    openaitool.NewCustomTool("json-extractor").ToTool(),
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

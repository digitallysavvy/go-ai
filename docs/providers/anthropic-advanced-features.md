# Anthropic Advanced Features

This guide covers advanced features available for Anthropic Claude models in the Go-AI SDK.

## Fast Mode

Fast mode enables 2.5x faster output token speeds for Claude Opus 4.6. This is particularly useful for applications requiring low-latency responses.

### Requirements
- **Model**: `claude-opus-4-6` only
- **API Version**: Latest Anthropic API

### Usage

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func main() {
    // Create Anthropic provider
    provider := anthropic.New(anthropic.Config{
        APIKey: "your-api-key",
    })

    // Create model with fast mode enabled
    options := &anthropic.ModelOptions{
        Speed: anthropic.SpeedFast,
    }

    model, err := provider.LanguageModelWithOptions("claude-opus-4-6", options)
    if err != nil {
        log.Fatal(err)
    }

    // Generate text with fast mode
    result, err := ai.GenerateText(context.Background(), model, "Quick question: What is 2+2?")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.Text)
}
```

### Speed Options

- `anthropic.SpeedFast`: Enable fast mode (2.5x faster)
- `anthropic.SpeedStandard`: Standard speed (default)

### Notes

- Fast mode is only available for `claude-opus-4-6`
- The API automatically adds the required beta header (`fast-mode-2026-02-01`)
- Fast mode may have slightly different response characteristics

## Adaptive Thinking

Adaptive thinking enables Claude to show its reasoning process before providing a final answer. This feature is available in two modes:

### 1. Adaptive Thinking (Opus 4.6+)

Claude dynamically adjusts reasoning effort based on task complexity.

```go
options := &anthropic.ModelOptions{
    Thinking: &anthropic.ThinkingConfig{
        Type: anthropic.ThinkingTypeAdaptive,
    },
}

model, err := provider.LanguageModelWithOptions("claude-opus-4-6", options)
```

### 2. Extended Thinking (Pre-Opus 4.6)

For older models, you can specify a thinking budget.

```go
budget := 5000  // Token budget for thinking

options := &anthropic.ModelOptions{
    Thinking: &anthropic.ThinkingConfig{
        Type:         anthropic.ThinkingTypeEnabled,
        BudgetTokens: &budget,
    },
}

model, err := provider.LanguageModelWithOptions("claude-sonnet-4", options)
```

### Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func main() {
    provider := anthropic.New(anthropic.Config{
        APIKey: "your-api-key",
    })

    // Enable adaptive thinking
    options := &anthropic.ModelOptions{
        Thinking: &anthropic.ThinkingConfig{
            Type: anthropic.ThinkingTypeAdaptive,
        },
    }

    model, err := provider.LanguageModelWithOptions("claude-opus-4-6", options)
    if err != nil {
        log.Fatal(err)
    }

    // Ask a complex question
    result, err := ai.GenerateText(context.Background(), model,
        "Solve this logic puzzle: Three switches control three light bulbs in another room...")
    if err != nil {
        log.Fatal(err)
    }

    // The response includes thinking content in the raw response
    fmt.Println("Answer:", result.Text)

    // Access thinking content from raw response
    if resp, ok := result.RawResponse.(anthropic.anthropicResponse); ok {
        for _, content := range resp.Content {
            if content.Type == "thinking" {
                fmt.Println("\nClaude's thinking:")
                fmt.Println(content.Thinking)
            }
        }
    }
}
```

### Thinking Types

- `anthropic.ThinkingTypeAdaptive`: Adaptive thinking (Opus 4.6+)
- `anthropic.ThinkingTypeEnabled`: Extended thinking with optional budget (older models)
- `anthropic.ThinkingTypeDisabled`: Disable thinking (default)

### Budget Tokens

For `ThinkingTypeEnabled`, you can optionally specify a token budget:

```go
budget := 5000
config := &anthropic.ThinkingConfig{
    Type:         anthropic.ThinkingTypeEnabled,
    BudgetTokens: &budget,  // Must be at least 1,024 tokens
}
```

- Minimum: 1,024 tokens
- Counts towards the `max_tokens` limit
- Optional for `ThinkingTypeEnabled`
- Not used for `ThinkingTypeAdaptive`

### Response Structure

When thinking is enabled, the response includes thinking content blocks:

```go
type anthropicResponse struct {
    Content []anthropicContent
    // ... other fields
}

type anthropicContent struct {
    Type      string  // "thinking", "text", etc.
    Thinking  string  // Thinking content
    Signature string  // Thinking signature
    Text      string  // Regular text content
}
```

## Combining Features

You can combine fast mode and adaptive thinking:

```go
options := &anthropic.ModelOptions{
    Speed: anthropic.SpeedFast,
    Thinking: &anthropic.ThinkingConfig{
        Type: anthropic.ThinkingTypeAdaptive,
    },
}

model, err := provider.LanguageModelWithOptions("claude-opus-4-6", options)
```

This provides:
- 2.5x faster output speeds (fast mode)
- Visible reasoning process (adaptive thinking)

## AWS Bedrock Support

Adaptive thinking is also supported for Claude models running on AWS Bedrock:

```go
import "github.com/digitallysavvy/go-ai/pkg/providers/bedrock"

provider := bedrock.New(bedrock.Config{
    AWSAccessKeyID:     "your-access-key",
    AWSSecretAccessKey: "your-secret-key",
    Region:            "us-east-1",
})

options := &bedrock.ModelOptions{
    Thinking: &bedrock.ThinkingConfig{
        Type: bedrock.ThinkingTypeAdaptive,
    },
}

model, err := provider.LanguageModelWithOptions(
    "anthropic.claude-opus-4-6-v1:0",
    options,
)
```

## Error Handling

The SDK handles various error conditions:

```go
result, err := ai.GenerateText(ctx, model, prompt)
if err != nil {
    if providererrors.IsRateLimitError(err) {
        // Handle rate limit
    } else if providererrors.IsInvalidRequestError(err) {
        // Handle invalid request (e.g., wrong model for fast mode)
    } else {
        // Handle other errors
    }
}
```

## Best Practices

### Fast Mode

1. **Use with Opus 4.6**: Fast mode only works with `claude-opus-4-6`
2. **Latency-sensitive applications**: Ideal for real-time chat, live assistance
3. **Monitor quality**: Test that fast mode meets your quality requirements

### Adaptive Thinking

1. **Complex tasks**: Use thinking for logic puzzles, math problems, strategic planning
2. **Debugging**: The thinking content helps understand Claude's reasoning
3. **Token budget**: For enabled mode, set budget based on task complexity
4. **Raw response**: Access thinking content through `result.RawResponse`

### Combined Features

1. **Evaluate trade-offs**: Fast mode + thinking may produce different results
2. **Test thoroughly**: Validate that combined features work for your use case
3. **Monitor costs**: Thinking tokens count towards usage

## Troubleshooting

### Fast Mode Not Working

- Verify you're using `claude-opus-4-6`
- Check API key has access to beta features
- Review error messages for specific issues

### Thinking Content Missing

- Verify thinking is enabled in model options
- Check `result.RawResponse` for thinking content
- Ensure model supports thinking (Claude 3+)

### Budget Token Issues

- Budget must be at least 1,024 tokens
- Budget counts towards `max_tokens` limit
- Only applicable for `ThinkingTypeEnabled`

## API Reference

### Model Options

```go
type ModelOptions struct {
    Speed    Speed            // Fast mode configuration
    Thinking *ThinkingConfig  // Thinking configuration
    // ... other options
}
```

### Thinking Configuration

```go
type ThinkingConfig struct {
    Type         ThinkingType  // Thinking mode
    BudgetTokens *int         // Optional token budget (enabled mode only)
}
```

### Thinking Types

```go
const (
    ThinkingTypeAdaptive  ThinkingType = "adaptive"   // Opus 4.6+
    ThinkingTypeEnabled   ThinkingType = "enabled"    // Older models
    ThinkingTypeDisabled  ThinkingType = "disabled"   // Default
)
```

### Speed Options

```go
const (
    SpeedFast     Speed = "fast"      // 2.5x faster (Opus 4.6 only)
    SpeedStandard Speed = "standard"  // Standard speed
)
```

## Version Compatibility

| Feature | Minimum Model | API Version |
|---------|--------------|-------------|
| Fast Mode | claude-opus-4-6 | 2023-06-01+ |
| Adaptive Thinking | claude-opus-4-6 | 2023-06-01+ |
| Extended Thinking | claude-3+ | 2023-06-01+ |

## Code Execution Tool (2026-01-20)

The code execution tool (`code-execution_20260120`) enables Claude to request client-side
code execution across three modes:

- **Python programmatic tool calls** — execute Python code that may trigger client-side tools
- **Bash command execution** — run shell commands and receive stdout/stderr/exit code
- **Text editor file operations** — view, create, or str_replace files

The required beta header `code-execution-20260120` is **automatically injected** when this
tool is included in the tool list.

### Requirements

- **Models**: `claude-opus-4-6`, `claude-sonnet-4-6`
- **Beta header**: injected automatically — no manual configuration needed

### Usage

```go
import (
    "context"
    "encoding/json"

    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/provider/types"
    "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
    anthropicTools "github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

func main() {
    prov := anthropic.New(anthropic.Config{APIKey: "your-api-key"})
    model, _ := prov.LanguageModel("claude-sonnet-4-6")

    // Create the tool with a client-side executor
    codeExecTool := anthropicTools.CodeExecution20260120()
    codeExecTool.Execute = func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
        inputJSON, _ := json.Marshal(input)
        parsed, err := anthropicTools.UnmarshalCodeExecutionInput(inputJSON)
        if err != nil {
            return nil, err
        }

        switch v := parsed.(type) {
        case *anthropicTools.BashCodeExecutionInput:
            stdout, stderr, code := runBash(v.Command)
            return &anthropicTools.BashExecutionResult{
                Type:       anthropicTools.CodeExecutionResultTypeBash,
                Stdout:     stdout,
                Stderr:     stderr,
                ReturnCode: code,
            }, nil

        case *anthropicTools.TextEditorInput:
            // handle view/create/str_replace based on v.Command
            return handleTextEditor(v), nil
        }
        return nil, fmt.Errorf("unsupported input type: %T", parsed)
    }

    result, _ := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
        Model:  model,
        Prompt: "List the files in /tmp using bash",
        Tools:  []types.Tool{codeExecTool},
    })
    fmt.Println(result.Text)
}
```

### Input Types

| Type constant | JSON `type` value | Fields |
|---|---|---|
| `CodeExecutionInputTypeProgrammatic` | `programmatic-tool-call` | `code string` |
| `CodeExecutionInputTypeBash` | `bash_code_execution` | `command string` |
| `CodeExecutionInputTypeTextEditor` | `text_editor_code_execution` | `command`, `path`, `file_text?`, `old_str?`, `new_str?` |

Use `anthropicTools.UnmarshalCodeExecutionInput(data)` to decode the discriminated union.

### Result Types

| Type constant | JSON `type` value | Description |
|---|---|---|
| `CodeExecutionResultTypeProgrammatic` | `code_execution_result` | Python execution result |
| `CodeExecutionResultTypeBash` | `bash_code_execution_result` | Bash execution result |
| `CodeExecutionResultTypeBashError` | `bash_code_execution_tool_result_error` | Bash execution error |
| `CodeExecutionResultTypeTextEditorError` | `text_editor_code_execution_tool_result_error` | Text editor error |
| `CodeExecutionResultTypeViewResult` | `text_editor_code_execution_view_result` | File view result |
| `CodeExecutionResultTypeCreateResult` | `text_editor_code_execution_create_result` | File create/update result |
| `CodeExecutionResultTypeStrReplaceResult` | `text_editor_code_execution_str_replace_result` | String replace result |

Error codes: `invalid_tool_input`, `unavailable`, `too_many_requests`, `execution_time_exceeded`,
`output_file_too_large` (bash), `file_not_found` (text editor).

Use `anthropicTools.UnmarshalCodeExecutionResult(data)` to decode result JSON.

### Complete Examples

- `examples/anthropic-code-execution-bash/` — bash execution with result handling
- `examples/anthropic-code-execution-text-editor/` — text editor view/create/str_replace flow

## Further Reading

- [Anthropic API Documentation](https://docs.anthropic.com/)
- [Claude Model Overview](https://www.anthropic.com/claude)
- [Extended Thinking Guide](https://www.anthropic.com/research/extended-thinking)

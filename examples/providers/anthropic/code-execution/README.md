# Code Execution Example

This example demonstrates how to use the Anthropic Code Execution tool with Claude Opus 4.5.

## Features

- Python code execution in sandboxed environment
- Bash command execution
- File creation and manipulation
- Data visualization
- Package installation

## Prerequisites

- Claude Opus 4.5 or Claude Sonnet 4.5 API access
- `ANTHROPIC_API_KEY` environment variable

## Usage

```bash
export ANTHROPIC_API_KEY=your_api_key_here
go run main.go
```

## Tool Capabilities

The code execution tool enables Claude to:

### Python Code Execution (Programmatic Tool Calling)
- Run Python code in a secure sandbox
- Install packages as needed
- Create visualizations with matplotlib
- Perform data analysis with pandas/numpy
- Trigger client-executed tools via `allowedCallers`

### Bash Code Execution
- Execute shell commands
- Run system operations
- Process files and directories
- Chain multiple commands

### Text Editor Code Execution
- View file contents
- Create new files
- Replace strings in files (str_replace)

## Input Types

The tool accepts three types of input (discriminated union):

### 1. Programmatic Tool Call
```json
{
  "type": "programmatic-tool-call",
  "code": "import matplotlib.pyplot as plt\n..."
}
```

### 2. Bash Code Execution
```json
{
  "type": "bash_code_execution",
  "command": "ls -la"
}
```

### 3. Text Editor Code Execution
```json
{
  "type": "text_editor_code_execution",
  "command": "view",
  "path": "/path/to/file"
}
```

## Output Types

The tool returns different output types based on the operation:

- `code_execution_result` - Python execution result
- `bash_code_execution_result` - Bash execution result
- `text_editor_code_execution_view_result` - File view result
- `text_editor_code_execution_create_result` - File creation result
- `text_editor_code_execution_str_replace_result` - String replacement result
- `bash_code_execution_tool_result_error` - Bash error
- `text_editor_code_execution_tool_result_error` - Text editor error

## Examples

### Data Analysis
```go
result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model: model,
    Messages: []ai.Message{
        {
            Role: ai.RoleUser,
            Content: ai.MessageContent{
                Text: stringPtr("Analyze this CSV data and create a visualization"),
            },
        },
    },
    Tools: map[string]ai.Tool{
        "codeExecution": tools.CodeExecution20250825(),
    },
    MaxSteps: intPtr(5),
})
```

### System Operations
```go
result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model: model,
    Messages: []ai.Message{
        {
            Role: ai.RoleUser,
            Content: ai.MessageContent{
                Text: stringPtr("Check system disk usage and memory"),
            },
        },
    },
    Tools: map[string]ai.Tool{
        "codeExecution": tools.CodeExecution20250825(),
    },
    MaxSteps: intPtr(5),
})
```

### File Management
```go
result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model: model,
    Messages: []ai.Message{
        {
            Role: ai.RoleUser,
            Content: ai.MessageContent{
                Text: stringPtr("Create a configuration file with these settings"),
            },
        },
    },
    Tools: map[string]ai.Tool{
        "codeExecution": tools.CodeExecution20250825(),
    },
    MaxSteps: intPtr(5),
})
```

## Important Notes

- This tool is executed by the Anthropic API in a secure sandbox
- Supports deferred results for programmatic tool calling
- Python packages can be installed on-demand
- File operations are sandboxed to the execution environment
- Image outputs (charts, plots) are automatically handled

## Error Handling

The tool may return these error codes:
- `invalid_tool_input` - Invalid input parameters
- `unavailable` - Service temporarily unavailable
- `too_many_requests` - Rate limit exceeded
- `execution_time_exceeded` - Execution timeout
- `output_file_too_large` - Output file exceeds size limit
- `file_not_found` - Requested file doesn't exist

## Best Practices

1. Use `MaxSteps` to allow multi-step operations
2. Handle errors gracefully with proper error checking
3. Validate outputs before using them
4. Be mindful of execution time limits
5. Keep file sizes reasonable

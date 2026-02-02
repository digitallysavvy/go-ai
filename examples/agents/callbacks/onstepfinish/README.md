# OnStepFinish Callback Example

This example demonstrates how to use the `OnStepFinish` callback to track and monitor agent execution step by step.

## What It Does

The `OnStepFinish` callback is called after each agent step completes, providing:
- Step number
- Tool calls made in the step
- Token usage for the step
- Finish reason
- Generated text
- Warnings (if any)

## Use Cases

- **Logging**: Track agent execution for debugging
- **Monitoring**: Track token usage and costs
- **Analytics**: Gather metrics on tool usage and step patterns
- **Debugging**: Understand how the agent makes decisions

## Running the Example

```bash
export OPENAI_API_KEY=your-api-key
cd examples/agents/callbacks/onstepfinish
go run main.go
```

## Expected Output

You'll see detailed information about each step:
```
=== Step 1 Completed ===
Tool calls: 1
Tokens used: 150
Finish reason: tool-calls
Text: Let me search for that information
Tools called:
  - search
=======================

=== Step 2 Completed ===
Tool calls: 1
Tokens used: 200
Finish reason: tool-calls
Text: Now let me calculate the square root
Tools called:
  - calculator
=======================

...
```

## Key Features

- Real-time step monitoring
- Token usage tracking
- Warning detection
- Tool call visibility

## See Also

- [Early Stopping Example](../early-stopping/) - Using callbacks for budget control
- [Agent Documentation](../../../../docs/03-agents/02-building-agents.mdx)

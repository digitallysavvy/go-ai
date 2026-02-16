# Early Stopping / Monitoring Example

This example demonstrates how to use the `OnStepFinish` callback for monitoring agent execution and implementing budget controls.

## What It Does

This example shows how to:
- Track cumulative token usage across steps
- Implement token budget limits
- Monitor for inefficient patterns
- Log warnings and issues
- Provide execution summaries

## Use Cases

- **Cost Control**: Stop execution when budget limits are reached
- **Resource Management**: Prevent runaway agent executions
- **Quality Control**: Detect and warn about inefficient tool usage
- **Compliance**: Ensure execution stays within defined limits

## Running the Example

```bash
export OPENAI_API_KEY=your-api-key
cd examples/agents/callbacks/early-stopping
go run main.go
```

## Expected Output

You'll see monitoring output for each step:
```
--- Step 1 ---
Tokens this step: 150
Total tokens: 150 / 1000

--- Step 2 ---
Tokens this step: 200
Total tokens: 350 / 1000

--- Step 3 ---
Tokens this step: 500
Total tokens: 850 / 1000
⚠️  WARNING: Approaching token limit (80%)

=== Execution Summary ===
Completed: true
Total steps: 3
Total tokens used: 850
Finish reason: stop
✅ Execution completed within budget
```

## Implementation Note

The current Go implementation of `OnStepFinish` doesn't return an error to stop execution. This example demonstrates the monitoring pattern. For actual early stopping, you can:

1. Use the `MaxSteps` configuration
2. Implement custom timeout logic with context
3. Use tool approval callbacks to reject expensive operations

## Monitoring Features

- Token usage tracking
- Budget limit warnings
- Tool call pattern detection
- Warning aggregation
- Execution summaries

## See Also

- [Basic OnStepFinish Example](../onstepfinish/) - Basic callback usage
- [Agent Configuration](../../../../docs/03-agents/02-building-agents.mdx)

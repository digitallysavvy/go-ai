# LangChain-Style Callbacks Example

This example demonstrates the LangChain-style callbacks introduced in v6.0.60+ of the Go-AI SDK. These callbacks provide fine-grained control over agent execution and align with LangChain's callback system for better interoperability.

## Features Demonstrated

### Chain Lifecycle Callbacks

- **OnChainStart**: Called when the agent begins execution
- **OnChainEnd**: Called when the agent completes successfully
- **OnChainError**: Called when the agent encounters an error

### Agent Decision Callbacks

- **OnAgentAction**: Called when the agent decides to take an action (e.g., call a tool)
- **OnAgentFinish**: Called when the agent reaches a final answer

### Tool Lifecycle Callbacks

- **OnToolStart**: Called immediately before a tool begins execution
- **OnToolEnd**: Called after a tool successfully completes
- **OnToolError**: Called when a tool execution fails

## Running the Example

```bash
export OPENAI_API_KEY=your_api_key_here
go run main.go
```

## Expected Output

The example will show detailed logging of:
1. Chain start with input details
2. Each agent step with actions taken
3. Tool calls with start/end/error states
4. Agent finish when a final answer is reached
5. Chain end with execution summary

## Use Cases

These callbacks are useful for:

- **Logging**: Track agent execution flow
- **Monitoring**: Measure performance and token usage
- **Debugging**: Understand agent decision-making
- **Integration**: Connect with LangChain/LangGraph workflows
- **Cost Tracking**: Monitor API usage per operation
- **Error Handling**: Implement custom error recovery
- **Analytics**: Collect execution metrics

## Comparison with Legacy Callbacks

The SDK still supports legacy callbacks (OnStepStart, OnStepFinish, OnToolCall, OnToolResult, OnFinish) which work alongside the new LangChain-style callbacks. Use:

- **Legacy callbacks** for simple step-based tracking
- **LangChain callbacks** for detailed lifecycle management and LangChain compatibility

## Next Steps

- Explore error handling patterns with OnChainError and OnToolError
- Implement custom retry logic using OnToolError
- Integrate with observability platforms using callback data
- Build LangChain-compatible agents with consistent callback patterns

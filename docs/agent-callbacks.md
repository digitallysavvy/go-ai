# Agent Callbacks

The Go-AI SDK provides comprehensive callback support for monitoring and controlling agent execution. Callbacks allow you to track progress, log intermediate results, monitor resource usage, and build real-time UI updates.

## OnStepFinish Callback

The `OnStepFinish` callback is triggered after each step in a multi-step agent execution. This is particularly useful for:

- **Progress tracking**: Monitor how many steps have been completed
- **Token usage monitoring**: Track costs in real-time
- **Debugging**: Inspect tool calls and results at each step
- **UI updates**: Build progress indicators and status displays
- **Logging**: Record detailed execution traces

### Signature

```go
OnStepFinish func(step types.StepResult)
```

### StepResult Structure

The `StepResult` passed to the callback contains complete information about the step:

```go
type StepResult struct {
    // Step number (1-indexed)
    StepNumber int

    // Text generated in this step
    Text string

    // Tool calls made in this step
    ToolCalls []ToolCall

    // Tool results from this step
    ToolResults []ToolResult

    // Finish reason for this step
    FinishReason FinishReason

    // Raw finish reason from the provider
    RawFinishReason string

    // Usage for this step (token counts)
    Usage Usage

    // Context management information (Anthropic-specific)
    ContextManagement interface{}

    // Warnings from this step
    Warnings []Warning

    // Response messages generated in this step
    ResponseMessages []ResponseMessage
}
```

### Basic Example

```go
agent := agent.NewToolLoopAgent(agent.AgentConfig{
    Model:  model,
    System: "You are a helpful assistant.",
    Tools:  tools,

    OnStepFinish: func(step types.StepResult) {
        fmt.Printf("Step %d complete: %s\n", step.StepNumber, step.Text)

        if len(step.ToolCalls) > 0 {
            fmt.Printf("Called %d tools\n", len(step.ToolCalls))
        }

        if step.Usage.InputTokens != nil {
            fmt.Printf("Used %d input tokens\n", *step.Usage.InputTokens)
        }
    },
})
```

### Advanced Example: Token Usage Tracking

```go
var totalCost float64
const INPUT_COST_PER_1K = 0.01   // $0.01 per 1K input tokens
const OUTPUT_COST_PER_1K = 0.03  // $0.03 per 1K output tokens

agent := agent.NewToolLoopAgent(agent.AgentConfig{
    Model: model,
    Tools: tools,

    OnStepFinish: func(step types.StepResult) {
        // Calculate cost for this step
        var stepCost float64

        if step.Usage.InputTokens != nil {
            inputCost := float64(*step.Usage.InputTokens) / 1000.0 * INPUT_COST_PER_1K
            stepCost += inputCost
        }

        if step.Usage.OutputTokens != nil {
            outputCost := float64(*step.Usage.OutputTokens) / 1000.0 * OUTPUT_COST_PER_1K
            stepCost += outputCost
        }

        totalCost += stepCost

        fmt.Printf("Step %d cost: $%.4f (Total: $%.4f)\n",
            step.StepNumber, stepCost, totalCost)
    },
})
```

### Example: Building a Progress UI

```go
type ProgressTracker struct {
    steps         []types.StepResult
    currentStep   int
    totalTokens   int64
    mu            sync.Mutex
}

func (p *ProgressTracker) OnStepFinish(step types.StepResult) {
    p.mu.Lock()
    defer p.mu.Unlock()

    p.steps = append(p.steps, step)
    p.currentStep = step.StepNumber

    if step.Usage.TotalTokens != nil {
        p.totalTokens += *step.Usage.TotalTokens
    }

    // Update UI (simplified example)
    fmt.Printf("\r[%d steps] Processing... %d tokens used",
        p.currentStep, p.totalTokens)
}

tracker := &ProgressTracker{}

agent := agent.NewToolLoopAgent(agent.AgentConfig{
    Model:        model,
    OnStepFinish: tracker.OnStepFinish,
})
```

### Example: Logging to File

```go
logFile, _ := os.Create("agent-execution.log")
defer logFile.Close()
logger := log.New(logFile, "", log.LstdFlags)

agent := agent.NewToolLoopAgent(agent.AgentConfig{
    Model: model,
    Tools: tools,

    OnStepFinish: func(step types.StepResult) {
        // Log structured data as JSON
        data, _ := json.MarshalIndent(step, "", "  ")
        logger.Printf("Step %d:\n%s\n", step.StepNumber, string(data))
    },
})
```

### Example: Early Termination Based on Steps

```go
const MAX_ALLOWED_STEPS = 5

stepCount := 0

agent := agent.NewToolLoopAgent(agent.AgentConfig{
    Model:    model,
    MaxSteps: 10,  // SDK maximum

    OnStepFinish: func(step types.StepResult) {
        stepCount++

        if stepCount >= MAX_ALLOWED_STEPS {
            fmt.Println("Warning: Approaching step limit")
        }

        // Note: In current implementation, OnStepFinish cannot stop execution
        // Use MaxSteps in AgentConfig for hard limits
    },
})
```

## Other Callbacks

### OnStepStart

Called before each step begins:

```go
OnStepStart func(stepNum int)
```

### OnToolCall

Called when the agent decides to call a tool:

```go
OnToolCall func(toolCall types.ToolCall)
```

### OnToolResult

Called when a tool execution completes:

```go
OnToolResult func(toolResult types.ToolResult)
```

### OnFinish

Called when the agent completes execution:

```go
OnFinish func(result *agent.AgentResult)
```

### LangChain-Style Callbacks

For compatibility with LangChain patterns:

```go
OnChainStart func(input string, messages []types.Message)
OnChainEnd   func(result *agent.AgentResult)
OnChainError func(err error)
OnAgentAction func(action agent.AgentAction)
OnAgentFinish func(finish agent.AgentFinish)
```

## Complete Example

See `/examples/agent/on-step-finish/main.go` for a complete working example demonstrating all features of the `OnStepFinish` callback.

## Best Practices

1. **Keep callbacks fast**: Callbacks are called synchronously during agent execution. Avoid blocking operations.

2. **Handle errors gracefully**: Don't panic in callbacks - log errors instead.

3. **Use goroutines for expensive operations**: If you need to do expensive work, spawn a goroutine:
   ```go
   OnStepFinish: func(step types.StepResult) {
       go func() {
           // Expensive operation like uploading logs
           uploadStepData(step)
       }()
   }
   ```

4. **Thread safety**: If callbacks share state, use proper synchronization:
   ```go
   var mu sync.Mutex
   var sharedState []types.StepResult

   OnStepFinish: func(step types.StepResult) {
       mu.Lock()
       defer mu.Unlock()
       sharedState = append(sharedState, step)
   }
   ```

5. **Monitor token usage**: Use `OnStepFinish` to track costs and set budgets.

6. **Debug with context**: `StepResult` includes warnings, raw responses, and context management info.

## Comparison with TypeScript AI SDK

The Go implementation closely follows the TypeScript AI SDK's callback patterns:

| TypeScript | Go | Notes |
|------------|-----|-------|
| `onStepFinish` | `OnStepFinish` | Same functionality |
| `onFinish` | `OnFinish` | Called at completion |
| `experimental_onToolCall` | `OnToolCall` | Stable in Go |

The `StepResult` structure matches the TypeScript `StreamStep` with all the same fields and behavior.

## See Also

- [Agent Guide](./agents.md)
- [Tool Integration](./tools.md)
- [Example: OnStepFinish](../examples/agent/on-step-finish/main.go)

# Anthropic Context Management Examples

This directory contains examples demonstrating Anthropic's context management feature for automatic conversation history cleanup.

## What is Context Management?

Context management is a beta feature from Anthropic that automatically removes or truncates content from conversation history to prevent hitting context window limits. This is particularly useful in:

- **Long agentic workflows** with many tool calls
- **Extended reasoning sessions** with thinking blocks
- **Multi-turn conversations** that accumulate context over time

## How It Works

Anthropic's API automatically analyzes your conversation and intelligently removes content based on the strategies you configure:

1. **clear_tool_uses**: Removes old tool call/result pairs while keeping final results
2. **clear_thinking**: Removes extended thinking blocks while keeping conclusions

You can use one or both strategies depending on your use case.

## Examples

### 1. Clear Tool Uses (`clear-tool-uses/`)

Demonstrates cleaning up tool calls in long agentic conversations.

**Use this when:**
- You have workflows with 10+ tool interactions
- Tool call history is cluttering context
- You need to keep assistant reasoning but remove tool noise

**Run:**
```bash
cd clear-tool-uses
ANTHROPIC_API_KEY=your-key-here go run main.go
```

### 2. Clear Thinking (`clear-thinking/`)

Demonstrates cleaning up extended thinking blocks in reasoning tasks.

**Use this when:**
- You're using Claude for complex analysis
- Thinking blocks are consuming too much context (10x token multiplier)
- You want conclusions without the thinking process

**Run:**
```bash
cd clear-thinking
ANTHROPIC_API_KEY=your-key-here go run main.go
```

### 3. Multiple Strategies (`multiple-strategies/`)

Demonstrates using **both** cleanup strategies for maximum optimization.

**Use this when:**
- You have very long conversations (100+ turns)
- You're combining agentic workflows with reasoning tasks
- You need maximum context savings

**Run:**
```bash
cd multiple-strategies
ANTHROPIC_API_KEY=your-key-here go run main.go
```

## Code Overview

### Basic Setup

```go
import (
    "github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

// Create provider
p := anthropic.New(anthropic.Config{
    APIKey: "your-api-key",
})

// Create model with context management
model, err := p.LanguageModelWithOptions("claude-sonnet-4", &anthropic.ModelOptions{
    ContextManagement: &anthropic.ContextManagement{
        Strategies: []string{anthropic.StrategyClearToolUses},
    },
})
```

### Accessing Context Management Results

```go
result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:    model,
    Messages: messages,
})

// Check if context was managed
if result.ContextManagement != nil {
    cm := result.ContextManagement.(*anthropic.ContextManagementResponse)

    fmt.Printf("Messages deleted: %d\n", cm.MessagesDeleted)
    fmt.Printf("Tool uses cleared: %d\n", cm.ToolUsesCleared)
    fmt.Printf("Thinking blocks cleared: %d\n", cm.ThinkingBlocksCleared)
}
```

## Strategy Selection Guide

| Scenario | Strategy | Why |
|----------|----------|-----|
| Agentic workflows | `clear_tool_uses` | Keeps reasoning, removes tool noise |
| Complex analysis | `clear_thinking` | Keeps conclusions, removes process |
| Long conversations | Both | Maximum context savings |
| Short conversations | None needed | No optimization required |

## Benefits

### 1. **Avoid Context Limits**
Long conversations can exceed 200K tokens. Context management automatically keeps you within limits.

### 2. **Reduce Costs**
Removing unnecessary content reduces token usage:
- Fewer input tokens = lower costs
- Especially important with thinking blocks (10x multiplier)

### 3. **Maintain Performance**
Smaller context windows = faster responses:
- Less data to process
- Quicker model responses
- Better user experience

### 4. **No Code Changes**
Just configure the strategies - Anthropic handles everything:
- Automatic detection of when to clean up
- Intelligent preservation of important content
- Seamless integration with existing code

## Response Structure

Context management data appears in your results:

```go
type ContextManagementResponse struct {
    MessagesDeleted       int // Complete messages removed
    MessagesTruncated     int // Messages partially truncated
    ToolUsesCleared       int // Tool calls removed (clear_tool_uses)
    ThinkingBlocksCleared int // Thinking blocks removed (clear_thinking)
}
```

## Beta Feature Notice

Context management is currently a **beta feature** from Anthropic:

- Requires beta header: `anthropic-beta: context-management-2025-01-22`
- Available on Claude 4.5+ models
- API may change in future releases
- The SDK automatically handles all beta header requirements

## Requirements

- Anthropic API key
- Claude 4.5 Sonnet, Opus, or Haiku model
- Go 1.21+

## Best Practices

1. **Start with one strategy** - Test `clear_tool_uses` OR `clear_thinking` first
2. **Monitor the stats** - Check `ContextManagementResponse` to see what's being cleared
3. **Use both for long sessions** - Combine strategies for maximum benefit
4. **Don't over-optimize** - Only use context management if you're hitting limits
5. **Test your workflow** - Ensure important context isn't being removed

## When NOT to Use

- ❌ Short conversations (< 10 turns)
- ❌ Single-shot requests
- ❌ When you need complete history
- ❌ Initial prototyping (add later if needed)

## Troubleshooting

### Context management not activating?

- Your conversation may not be long enough yet
- Anthropic only activates it when necessary
- Check that you're using Claude 4.5+

### Important information being removed?

- Reconsider which strategy you're using
- `clear_tool_uses` is safer (only removes tool internals)
- `clear_thinking` is more aggressive (removes reasoning)
- Consider using only one strategy instead of both

### Getting errors?

- Verify your API key is valid
- Check you're using a supported model
- Ensure you have the latest SDK version

## Learn More

- [Anthropic Documentation](https://docs.anthropic.com/)
- [Go-AI SDK Documentation](../../docs/)
- [Context Management PRD](../../../../prds/p1-high/P1-1-Anthropic-Context-Management.md)

## Support

For issues or questions:
- [GitHub Issues](https://github.com/digitallysavvy/go-ai/issues)
- [Anthropic API Discord](https://discord.gg/anthropic)

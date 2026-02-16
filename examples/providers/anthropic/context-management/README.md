# Anthropic Context Management Examples

This directory contains examples demonstrating Anthropic's context management feature for automatic conversation history optimization.

## What is Context Management?

Context management is a beta feature from Anthropic that automatically manages conversation history to prevent hitting context window limits. This is particularly useful in:

- **Long agentic workflows** with many tool calls
- **Extended reasoning sessions** with thinking blocks
- **Multi-turn conversations** that accumulate context over time
- **Near-limit conversations** that need aggressive compaction

## How It Works

Anthropic's API provides three types of context management edits that you can configure:

### 1. Clear Tool Uses (`clear_tool_uses_20250919`)
Removes old tool call details while keeping recent ones and final results.

**Configuration options:**
- `trigger`: When to activate (token count or tool use count)
- `keep`: How many recent tool uses to preserve
- `clearAtLeast`: Minimum tokens to clear
- `clearToolInputs`: Whether to clear tool input content
- `excludeTools`: Specific tools to never clear

### 2. Clear Thinking (`clear_thinking_20251015`)
Removes extended thinking blocks while keeping conclusions.

**Configuration options:**
- `keep`: Keep "all" or specify number of recent thinking turns

### 3. Compact (`compact_20260112`)
Summarizes the entire conversation history (most aggressive).

**Configuration options:**
- `trigger`: Token threshold to activate compaction
- `pauseAfterCompaction`: Pause to inspect compacted history
- `instructions`: Custom guidance for compaction

## Examples

### 1. Clear Tool Uses (`clear-tool-uses/`)

Demonstrates cleaning up tool calls in long agentic conversations.

**Configuration:**
- Trigger: Input tokens > 10,000
- Keep: Last 3 tool uses
- Clear at least: 5,000 tokens

**Run:**
```bash
cd clear-tool-uses
ANTHROPIC_API_KEY=your-key-here go run main.go
```

### 2. Clear Thinking (`clear-thinking/`)

Demonstrates cleaning up extended thinking blocks in reasoning tasks.

**Configuration:**
- Keep: Last 2 thinking turns

**Run:**
```bash
cd clear-thinking
ANTHROPIC_API_KEY=your-key-here go run main.go
```

### 3. Compact (`compact/`)

Demonstrates conversation compaction for extremely long conversations.

**Configuration:**
- Trigger: Input tokens > 50,000
- Instructions: Custom compaction guidance

**Run:**
```bash
cd compact
ANTHROPIC_API_KEY=your-key-here go run main.go
```

### 4. Multiple Edit Types (`multiple-strategies/`)

Demonstrates using **all three** edit types for maximum optimization.

**Configuration:**
- Clear tool uses: Trigger at 10K tokens, keep 3
- Clear thinking: Keep last 2 turns
- Compact: Trigger at 50K tokens

**Run:**
```bash
cd multiple-strategies
ANTHROPIC_API_KEY=your-key-here go run main.go
```

### 5. Pause/Resume (`pause-resume/`)

Demonstrates pausing after compaction to inspect results.

**Configuration:**
- Compact with `pauseAfterCompaction: true`
- Allows inspection before resuming

**Run:**
```bash
cd pause-resume
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
model, err := p.LanguageModelWithOptions("claude-sonnet-4-5", &anthropic.ModelOptions{
    ContextManagement: &anthropic.ContextManagement{
        Edits: []anthropic.ContextManagementEdit{
            // Clear tool uses after 10K tokens, keep last 3
            anthropic.NewClearToolUsesEdit().
                WithInputTokensTrigger(10000).
                WithKeepToolUses(3),

            // Clear thinking blocks, keep last 2 turns
            anthropic.NewClearThinkingEdit().
                WithKeepRecentTurns(2),

            // Compact at 50K tokens
            anthropic.NewCompactEdit().
                WithTrigger(50000),
        },
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
    cmr := result.ContextManagement.(*anthropic.ContextManagementResponse)

    // Check each applied edit
    for _, edit := range cmr.AppliedEdits {
        switch e := edit.(type) {
        case *anthropic.AppliedClearToolUsesEdit:
            fmt.Printf("Cleared %d tool uses, freed %d tokens\n",
                e.ClearedToolUses, e.ClearedInputTokens)

        case *anthropic.AppliedClearThinkingEdit:
            fmt.Printf("Cleared %d thinking turns, freed %d tokens\n",
                e.ClearedThinkingTurns, e.ClearedInputTokens)

        case *anthropic.AppliedCompactEdit:
            fmt.Println("Conversation compacted")
        }
    }
}
```

## Edit Type Selection Guide

| Scenario | Edit Type | Why |
|----------|----------|-----|
| Agentic workflows | ClearToolUses | Keeps reasoning, removes tool noise |
| Complex analysis | ClearThinking | Keeps conclusions, removes process |
| Very long conversations | Compact | Most aggressive space-saving |
| Maximum optimization | All three | Progressive optimization |

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

### 4. **Fine-Grained Control**
Configure exactly when and how context is managed:
- Set specific triggers
- Control what to keep
- Customize compaction instructions

## Response Structure

Context management data appears in your results:

```go
type ContextManagementResponse struct {
    AppliedEdits []AppliedEdit // All edits that were applied
}

type AppliedClearToolUsesEdit struct {
    Type               string // "clear_tool_uses_20250919"
    ClearedToolUses    int    // Number of tool uses cleared
    ClearedInputTokens int    // Tokens freed
}

type AppliedClearThinkingEdit struct {
    Type                 string // "clear_thinking_20251015"
    ClearedThinkingTurns int    // Number of thinking turns cleared
    ClearedInputTokens   int    // Tokens freed
}

type AppliedCompactEdit struct {
    Type string // "compact_20260112"
}
```

## Beta Feature Notice

Context management is currently a **beta feature** from Anthropic:

- Requires beta headers:
  - `context-management-2025-06-27` for clear_tool_uses and clear_thinking
  - `compact-2026-01-12` for compact edits
- Available on Claude 4.5+ models
- API may change in future releases
- The SDK automatically handles all beta header requirements

## Requirements

- Anthropic API key
- Claude 4.5 Sonnet, Opus, or Haiku model
- Go 1.21+

## Best Practices

1. **Start with one edit type** - Test ClearToolUses OR ClearThinking first
2. **Monitor the results** - Check `ContextManagementResponse` to see what's being cleared
3. **Use multiple edits for long sessions** - Combine edit types for maximum benefit
4. **Set appropriate triggers** - Don't trigger too early or too late
5. **Test your workflow** - Ensure important context isn't being removed

## Advanced Features

### Iterations with Compaction

When compaction occurs, the API returns usage broken down by iteration:

```go
if rawUsage, ok := result.Usage.Raw.(map[string]interface{}); ok {
    if iterations, ok := rawUsage["iterations"].([]interface{}); ok {
        // First iteration: compaction
        // Second iteration: actual message generation
        // Total tokens consumed = sum of all iterations
    }
}
```

### Pause After Compaction

```go
anthropic.NewCompactEdit().
    WithTrigger(50000).
    WithPauseAfterCompaction(true) // Pause to inspect
```

When enabled:
1. Compaction occurs
2. Request pauses with compacted history
3. You can inspect/modify the history
4. Resume with next request

### Custom Compaction Instructions

```go
anthropic.NewCompactEdit().
    WithTrigger(50000).
    WithInstructions("Preserve all technical decisions and API design choices")
```

## When NOT to Use

- ❌ Short conversations (< 10 turns)
- ❌ Single-shot requests
- ❌ When you need complete history
- ❌ Initial prototyping (add later if needed)

## Troubleshooting

### Context management not activating?

- Your conversation may not be long enough yet
- Triggers haven't been reached
- Check that you're using Claude 4.5+

### Important information being removed?

- Adjust trigger thresholds
- Increase `keep` values
- Use `excludeTools` for critical tools
- Add custom compaction instructions

### Getting errors?

- Verify your API key is valid
- Check you're using a supported model
- Ensure you have the latest SDK version

## Learn More

- [Anthropic Documentation](https://docs.anthropic.com/)
- [Go-AI SDK Documentation](../../docs/)
- [Anthropic Provider Guide](../../../../docs/05-providers/03-anthropic.mdx)

## Support

For issues or questions:
- [GitHub Issues](https://github.com/digitallysavvy/go-ai/issues)
- [Anthropic Discord](https://discord.gg/anthropic)

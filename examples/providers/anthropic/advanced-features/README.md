# Anthropic Advanced Features Example

This example demonstrates the advanced features available for Anthropic Claude models:

1. **Fast Mode**: 2.5x faster output speeds (Opus 4.6 only)
2. **Adaptive Thinking**: Claude shows its reasoning process dynamically
3. **Extended Thinking**: Thinking with token budget (older models)
4. **Combined Features**: Using fast mode and thinking together

## Prerequisites

- Go 1.24+
- Anthropic API key with access to Claude Opus 4.6

## Setup

```bash
export ANTHROPIC_API_KEY="your-api-key-here"
```

## Running the Example

```bash
cd examples/providers/anthropic/advanced-features
go run main.go
```

## Example Output

```
=== Anthropic Advanced Features Demo ===

--- Demo 1: Fast Mode ---
Fast mode provides 2.5x faster output speeds (Opus 4.6 only)

Response: Paris
Tokens used: 15 input, 3 output

--- Demo 2: Adaptive Thinking ---
Adaptive thinking shows Claude's reasoning process

Claude's Thinking Process:
To solve this puzzle, I need to think about what information each action gives me...
[Claude's reasoning continues]

Answer: Here's the solution: First, turn on switch 1 for 5 minutes, then turn it off...
Tokens used: 67 input, 234 output

--- Demo 3: Extended Thinking with Budget ---
For models before Opus 4.6, specify a thinking budget

Response: Let me calculate the compound interest step by step...
Tokens used: 45 input, 187 output
Thinking budget: 3000 tokens

--- Demo 4: Combined Features ---
Fast mode + adaptive thinking for rapid, reasoned responses

Response: Based on the factors you mentioned, I recommend option C...
Tokens used: 52 input, 156 output
Features: Fast mode + Adaptive thinking
```

## Features Demonstrated

### 1. Fast Mode

- Enables 2.5x faster output speeds
- Only available for `claude-opus-4-6`
- Ideal for latency-sensitive applications

```go
options := &anthropic.ModelOptions{
    Speed: anthropic.SpeedFast,
}
```

### 2. Adaptive Thinking

- Claude dynamically adjusts reasoning effort
- Shows thinking process before final answer
- Available for Opus 4.6 and newer

```go
options := &anthropic.ModelOptions{
    Thinking: &anthropic.ThinkingConfig{
        Type: anthropic.ThinkingTypeAdaptive,
    },
}
```

### 3. Extended Thinking with Budget

- For models before Opus 4.6
- Specify maximum thinking tokens
- Minimum 1,024 tokens required

```go
budget := 3000
options := &anthropic.ModelOptions{
    Thinking: &anthropic.ThinkingConfig{
        Type:         anthropic.ThinkingTypeEnabled,
        BudgetTokens: &budget,
    },
}
```

### 4. Combined Features

- Use fast mode and thinking together
- Fast responses with visible reasoning
- Requires Opus 4.6

```go
options := &anthropic.ModelOptions{
    Speed: anthropic.SpeedFast,
    Thinking: &anthropic.ThinkingConfig{
        Type: anthropic.ThinkingTypeAdaptive,
    },
}
```

## Use Cases

### Fast Mode
- Real-time chat applications
- Live customer support
- Quick Q&A systems
- Auto-complete features

### Adaptive Thinking
- Complex problem-solving
- Logic puzzles and games
- Strategic planning
- Educational tutoring
- Debugging and analysis

### Combined
- Real-time decision support
- Interactive problem solving
- Fast strategic advice
- Live reasoning demonstrations

## Notes

- Fast mode requires `claude-opus-4-6` model
- Thinking content is available in `result.RawResponse`
- Token budgets count towards `max_tokens` limit
- Beta features require appropriate API access

## Error Handling

The example includes basic error handling. In production:

1. Check model compatibility (fast mode requires Opus 4.6)
2. Validate thinking budget (minimum 1,024 tokens)
3. Handle rate limits appropriately
4. Monitor token usage and costs

## Further Reading

- [Go-AI Documentation](../../docs/providers/anthropic-advanced-features.md)
- [Anthropic API Docs](https://docs.anthropic.com/)
- [Claude Model Guide](https://www.anthropic.com/claude)

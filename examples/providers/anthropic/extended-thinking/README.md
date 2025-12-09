# Anthropic Extended Thinking

Demonstrates Claude's extended thinking mode for complex reasoning tasks.

## Features

- **Deep reasoning** before responding
- **Multi-step problem solving**
- **Self-correction** and verification
- **Transparent thinking process**
- **Higher accuracy** on complex tasks

## How It Works

Extended thinking allows Claude to:
1. Reason through problems step-by-step
2. Consider multiple approaches
3. Verify answers before responding
4. Show reasoning process

## Prerequisites

- Anthropic API key
- Claude Sonnet 4 or Opus 4 recommended

## Setup

```bash
export ANTHROPIC_API_KEY=sk-ant-...
```

## Running

```bash
cd examples/providers/anthropic/extended-thinking
go run main.go
```

## Use Cases

1. **Math Problems** - Complex calculations
2. **Logic Puzzles** - Multi-constraint problems
3. **Code Analysis** - Understanding complex code
4. **Strategic Planning** - Multi-step decision making
5. **Debugging** - Root cause analysis

## Best Practices

1. **Encourage thinking** in system prompts
2. **Allow time** for reasoning (higher latency)
3. **Request step-by-step** explanations
4. **Verify answers** in the prompt

## Documentation

- [Anthropic Claude Models](https://docs.anthropic.com/claude/docs/models-overview)
- [Go AI SDK Docs](../../../../docs)

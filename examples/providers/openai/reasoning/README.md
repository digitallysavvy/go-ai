# OpenAI Reasoning Models (o1 Series)

Demonstrates OpenAI's o1 reasoning models optimized for complex problem-solving.

## Models

- **o1-preview**: Most capable reasoning model for complex tasks
- **o1-mini**: Faster, more cost-effective for simpler reasoning tasks

## Key Characteristics

- **No temperature/top_p**: Fixed reasoning approach
- **No system messages**: Direct prompts only
- **Reasoning tokens**: Separate token tracking for internal reasoning
- **Best for**: Math, logic, code analysis, complex problem-solving

## Prerequisites

- OpenAI API key
- Access to o1 models (may require waitlist)

## Setup

```bash
export OPENAI_API_KEY=sk-...
```

## Running

```bash
cd examples/providers/openai/reasoning
go run main.go
```

## Use Cases

1. **Complex Math Problems** - Multi-step calculations
2. **Logic Puzzles** - Constraint satisfaction problems
3. **Code Optimization** - Performance analysis and improvements
4. **Scientific Reasoning** - Multi-step analysis
5. **Strategic Planning** - Complex decision trees

## Documentation

- [OpenAI o1 Models](https://platform.openai.com/docs/models/o1)
- [Go AI SDK Docs](../../../../docs)

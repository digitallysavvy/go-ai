# Math Agent Example

Demonstrates an AI agent with multiple math tools for complex problem solving.

## Features

- **Multiple tools** (calculator, sqrt, power, factorial)
- **Multi-step reasoning** with tool chaining
- **Step-by-step visualization** of problem solving
- **Tool result tracking**
- **Reasoning transparency**

## Prerequisites

- Go 1.21+
- OpenAI API key (GPT-4 recommended)

## Setup

```bash
export OPENAI_API_KEY=sk-...
```

## Running

```bash
cd examples/agents/math-agent
go run main.go
```

## How It Works

The agent:
1. Receives a math problem
2. Reasons about what tools to use
3. Calls tools in sequence
4. Uses results to solve the problem
5. Provides final answer with explanation

## Available Tools

- **calculator**: add, subtract, multiply, divide
- **sqrt**: square root
- **power**: exponentiation
- **factorial**: n!

## Example Output

```
Problem 1: Calculate the square root of 144 plus 8
  Step 1:
    Tool: sqrt
    Args: map[number:144]
    Result: map[input:144 result:12]
  Step 2:
    Tool: calculator
    Args: map[a:12 b:8 operation:add]
    Result: map[operands:[12 8] operation:add result:20]

Final Answer: The square root of 144 is 12, and 12 + 8 = 20
Total Steps: 2
Token Usage: 450
```

## Use Cases

1. **Complex Calculations** - Multi-step math problems
2. **Educational Tools** - Show step-by-step solutions
3. **Scientific Computing** - Chain mathematical operations
4. **Data Analysis** - Perform calculations on data

## Documentation

- [Go AI SDK Agents](../../../docs)
- [Tool Calling Guide](../../../docs/tool-calling.md)

# Text Generation Example

This example demonstrates the core text generation capabilities of the Go AI SDK.

## Features Demonstrated

1. **Simple Text Generation** - Basic text completion
2. **Streaming Text** - Real-time streaming responses
3. **Tool Calling** - Using tools to extend AI capabilities

## Prerequisites

- Go 1.21 or higher
- OpenAI API key

## Setup

1. Set your OpenAI API key:

```bash
export OPENAI_API_KEY=sk-...
```

2. Run the example:

```bash
go run main.go
```

## What You'll Learn

- How to create a provider and get a language model
- How to generate text with `GenerateText()`
- How to stream responses with `StreamText()` and `Chunks()`
- How to define and use tools for function calling
- How to handle multi-step reasoning with `MaxSteps`

## Expected Output

The example will demonstrate:
- A programming joke from simple text generation
- A streaming haiku about Go
- Tool usage for weather information with multi-step execution

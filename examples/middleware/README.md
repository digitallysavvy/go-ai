# Middleware Examples

This directory contains examples demonstrating the use of middleware in the Go-AI SDK.

## Overview

Middleware allows you to wrap language models with additional behavior, transforming inputs and outputs without modifying the core model implementation.

## Available Middleware

### 1. Extract JSON Middleware (`extract_json_example.go`)

**Purpose**: Automatically strips markdown code fences from JSON responses.

**Use Cases**:
- Working with models that wrap JSON in markdown (```json ... ```)
- Using structured output with Output.object()
- Extracting JSON from mixed-format responses

**Example**:
```go
middleware := middleware.ExtractJSONMiddleware(nil)
wrapped := middleware.WrapLanguageModel(model, []*middleware.LanguageModelMiddleware{middleware}, nil, nil)
```

### 2. Extract Reasoning Middleware (`extract_reasoning_example.go`)

**Purpose**: Extracts XML-tagged reasoning sections from generated text.

**Use Cases**:
- OpenAI o1 models (use `tagName: "reasoning"`)
- Anthropic Claude with thinking blocks (use `tagName: "think"`)
- Separating reasoning from final responses
- Analyzing model thought processes

**Example**:
```go
middleware := middleware.ExtractReasoningMiddleware(&middleware.ExtractReasoningOptions{
    TagName:   "think",
    Separator: "\n",
})
wrapped := middleware.WrapLanguageModel(model, []*middleware.LanguageModelMiddleware{middleware}, nil, nil)
```

**Streaming Output**:
- Reasoning chunks: `ChunkTypeReasoning` with `chunk.Reasoning`
- Text chunks: `ChunkTypeText` with `chunk.Text`

### 3. Simulate Streaming Middleware (`simulate_streaming_example.go`)

**Purpose**: Converts non-streaming responses into simulated streams.

**Use Cases**:
- Testing streaming behavior in development
- Supporting providers without native streaming
- Providing consistent streaming interface across all providers
- Converting batch responses for UI compatibility

**Example**:
```go
middleware := middleware.SimulateStreamingMiddleware()
wrapped := middleware.WrapLanguageModel(model, []*middleware.LanguageModelMiddleware{middleware}, nil, nil)

// Now you can use DoStream() even if the provider doesn't support it
stream, _ := wrapped.DoStream(ctx, opts)
```

### 4. Add Tool Input Examples Middleware (`add_tool_examples_example.go`)

**Purpose**: Appends input examples to tool descriptions for better LLM tool calling accuracy.

**Use Cases**:
- Improving tool calling accuracy
- Working with providers that don't support `inputExamples` natively
- Providing clear usage patterns for complex tools
- Reducing token usage by converting examples to text

**Example**:
```go
middleware := middleware.AddToolInputExamplesMiddleware(&middleware.AddToolInputExamplesOptions{
    Prefix: "Input Examples:",
    Remove: true, // Remove inputExamples after conversion
})
wrapped := middleware.WrapLanguageModel(model, []*middleware.LanguageModelMiddleware{middleware}, nil, nil)
```

## Running Examples

Each example is a standalone Go program. To run an example:

1. Set your API key:
```bash
export OPENAI_API_KEY=your-key-here
# or
export ANTHROPIC_API_KEY=your-key-here
```

2. Run the example:
```bash
go run extract_json_example.go
```

## Combining Multiple Middleware

You can chain multiple middleware together:

```go
wrappedModel := middleware.WrapLanguageModel(
    model,
    []*middleware.LanguageModelMiddleware{
        middleware.ExtractJSONMiddleware(nil),
        middleware.ExtractReasoningMiddleware(&middleware.ExtractReasoningOptions{
            TagName: "think",
        }),
        middleware.SimulateStreamingMiddleware(),
    },
    nil,
    nil,
)
```

Middleware is applied in order: the first middleware transforms input first, and the last middleware wraps directly around the model.

## Custom Middleware

You can create custom middleware by implementing the `LanguageModelMiddleware` struct:

```go
customMiddleware := &middleware.LanguageModelMiddleware{
    SpecificationVersion: "v3",

    TransformParams: func(ctx context.Context, callType string, params *provider.GenerateOptions, model provider.LanguageModel) (*provider.GenerateOptions, error) {
        // Transform parameters before generation
        return params, nil
    },

    WrapGenerate: func(ctx context.Context, doGenerate func() (*types.GenerateResult, error), doStream func() (provider.TextStream, error), params *provider.GenerateOptions, model provider.LanguageModel) (*types.GenerateResult, error) {
        // Wrap non-streaming generation
        result, err := doGenerate()
        // Transform result
        return result, err
    },

    WrapStream: func(ctx context.Context, doGenerate func() (*types.GenerateResult, error), doStream func() (provider.TextStream, error), params *provider.GenerateOptions, model provider.LanguageModel) (provider.TextStream, error) {
        // Wrap streaming generation
        stream, err := doStream()
        // Return wrapped stream
        return stream, err
    },
}
```

## More Information

For more details on middleware implementation, see:
- `/pkg/middleware/` - Middleware implementations
- `/pkg/middleware/*_test.go` - Unit tests with usage examples
- [Main README](../../README.md) - Project overview

# TypeScript to Go Translation Patterns

**Purpose:** Guide for adapting TypeScript AI SDK examples to Go
**Last Updated:** 2024-12-08

## Overview

When translating TypeScript examples to Go, you need to understand both the API differences and Go's idioms. This guide provides patterns for common translations.

## Table of Contents

1. [Basic API Patterns](#basic-api-patterns)
2. [Streaming Patterns](#streaming-patterns)
3. [Type System Differences](#type-system-differences)
4. [Error Handling](#error-handling)
5. [Async/Promises vs Context](#asyncpromises-vs-context)
6. [Tool Definitions](#tool-definitions)
7. [Agent Patterns](#agent-patterns)
8. [Middleware Patterns](#middleware-patterns)
9. [HTTP Server Patterns](#http-server-patterns)
10. [Common Pitfalls](#common-pitfalls)

---

## Basic API Patterns

### Text Generation

**TypeScript:**
```typescript
import { generateText } from 'ai';
import { openai } from '@ai-sdk/openai';

const result = await generateText({
  model: openai('gpt-4'),
  prompt: 'Write a haiku about programming'
});

console.log(result.text);
console.log(result.usage);
```

**Go:**
```go
import (
    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

provider := openai.New(openai.Config{
    APIKey: os.Getenv("OPENAI_API_KEY"),
})
model, _ := provider.LanguageModel("gpt-4")

result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:  model,
    Prompt: "Write a haiku about programming",
})
if err != nil {
    log.Fatal(err)
}

fmt.Println(result.Text)
fmt.Println(result.Usage)
```

**Key Differences:**
- Go requires explicit error handling
- Go uses `context.Context` instead of promises
- Go separates provider creation from model retrieval
- Go uses structs for options instead of object literals

---

## Streaming Patterns

### Basic Streaming

**TypeScript:**
```typescript
import { streamText } from 'ai';

const result = streamText({
  model: openai('gpt-4'),
  prompt: 'Count to 10'
});

for await (const chunk of result.textStream) {
  process.stdout.write(chunk);
}
```

**Go:**
```go
stream, err := ai.StreamText(ctx, ai.StreamTextOptions{
    Model:  model,
    Prompt: "Count to 10",
})
if err != nil {
    log.Fatal(err)
}

for chunk := range stream.Chunks() {
    if chunk.Type == provider.ChunkTypeText {
        fmt.Print(chunk.Text)
    }
}

if err := stream.Err(); err != nil {
    log.Printf("Stream error: %v", err)
}
```

**Key Differences:**
- Go uses channels (`range stream.Chunks()`) instead of async iterators
- Go requires checking chunk type explicitly
- Go needs error checking after stream completes
- TypeScript uses `for await...of`, Go uses `for...range`

### HTTP SSE Streaming

**TypeScript (Next.js):**
```typescript
import { streamText } from 'ai';

export async function POST(req: Request) {
  const { messages } = await req.json();

  const result = streamText({
    model: openai('gpt-4'),
    messages,
  });

  return result.toDataStreamResponse();
}
```

**Go (Gin):**
```go
func streamHandler(c *gin.Context) {
    var req struct {
        Messages []types.Message `json:"messages"`
    }
    if err := c.BindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }

    // Set SSE headers
    c.Header("Content-Type", "text/event-stream")
    c.Header("Cache-Control", "no-cache")
    c.Header("Connection", "keep-alive")

    stream, err := ai.StreamText(c.Request.Context(), ai.StreamTextOptions{
        Model:    model,
        Messages: req.Messages,
    })
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    // Stream chunks as SSE
    for chunk := range stream.Chunks() {
        if chunk.Type == provider.ChunkTypeText {
            fmt.Fprintf(c.Writer, "data: %s\n\n", chunk.Text)
            c.Writer.(http.Flusher).Flush()
        }
    }
}
```

**Key Differences:**
- Go requires manual SSE header setup
- Go needs explicit flush calls
- Go uses `c.Writer` directly for streaming
- TypeScript has built-in `toDataStreamResponse()`

---

## Type System Differences

### Message Types

**TypeScript:**
```typescript
const messages = [
  { role: 'user', content: 'Hello!' },
  { role: 'assistant', content: 'Hi there!' }
];
```

**Go:**
```go
messages := []types.Message{
    {
        Role: types.RoleUser,
        Content: []types.ContentPart{
            types.TextContent{Text: "Hello!"},
        },
    },
    {
        Role: types.RoleAssistant,
        Content: []types.ContentPart{
            types.TextContent{Text: "Hi there!"},
        },
    },
}
```

**Key Differences:**
- Go uses typed constants for roles (`types.RoleUser`, `types.RoleAssistant`)
- Go's `Content` is `[]ContentPart` for multimodal support
- Go requires explicit type construction
- TypeScript uses string literals for roles

### Structured Output

**TypeScript:**
```typescript
import { z } from 'zod';
import { generateObject } from 'ai';

const schema = z.object({
  name: z.string(),
  age: z.number(),
  hobbies: z.array(z.string())
});

const result = await generateObject({
  model: openai('gpt-4'),
  schema,
  prompt: 'Generate a person'
});

console.log(result.object.name);
```

**Go:**
```go
import "github.com/digitallysavvy/go-ai/pkg/schema"

personSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "name": map[string]interface{}{"type": "string"},
        "age":  map[string]interface{}{"type": "number"},
        "hobbies": map[string]interface{}{
            "type":  "array",
            "items": map[string]interface{}{"type": "string"},
        },
    },
    "required": []string{"name", "age", "hobbies"},
})

result, err := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
    Model:  model,
    Schema: personSchema,
    Prompt: "Generate a person",
})

// Type assert the result
if personMap, ok := result.Object.(map[string]interface{}); ok {
    fmt.Println(personMap["name"])
}
```

**Key Differences:**
- TypeScript uses Zod for schemas, Go uses JSON Schema maps
- Go requires type assertion for accessing object fields
- TypeScript has compile-time type safety, Go uses runtime type assertions
- Go's schema is more verbose but flexible

---

## Error Handling

### TypeScript Pattern

**TypeScript:**
```typescript
try {
  const result = await generateText({
    model: openai('gpt-4'),
    prompt: 'Hello'
  });
  console.log(result.text);
} catch (error) {
  console.error('Error:', error);
}
```

**Go Pattern:**
```go
result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:  model,
    Prompt: "Hello",
})
if err != nil {
    log.Printf("Error: %v", err)
    return
}

fmt.Println(result.Text)
```

**Key Differences:**
- Go uses explicit error returns, not exceptions
- Go checks errors immediately after function calls
- TypeScript uses try/catch blocks
- Go encourages early returns for errors

### Stream Error Handling

**TypeScript:**
```typescript
try {
  for await (const chunk of result.textStream) {
    process.stdout.write(chunk);
  }
} catch (error) {
  console.error('Stream error:', error);
}
```

**Go:**
```go
for chunk := range stream.Chunks() {
    if chunk.Type == provider.ChunkTypeText {
        fmt.Print(chunk.Text)
    }
}

// Check for errors AFTER stream completes
if err := stream.Err(); err != nil {
    log.Printf("Stream error: %v", err)
}
```

**Key Differences:**
- Go checks errors after stream completes
- TypeScript catches errors during iteration
- Go uses `stream.Err()` pattern
- Channel closes on error in Go

---

## Async/Promises vs Context

### Cancellation

**TypeScript:**
```typescript
const controller = new AbortController();

setTimeout(() => controller.abort(), 5000);

const result = await generateText({
  model: openai('gpt-4'),
  prompt: 'Long task',
  abortSignal: controller.signal
});
```

**Go:**
```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:  model,
    Prompt: "Long task",
})
if err == context.DeadlineExceeded {
    log.Println("Request timed out")
}
```

**Key Differences:**
- Go uses `context.Context` for cancellation
- TypeScript uses `AbortController`
- Go's context propagates through call stack automatically
- TypeScript requires passing signal explicitly

### Parallel Requests

**TypeScript:**
```typescript
const [result1, result2, result3] = await Promise.all([
  generateText({ model: openai('gpt-4'), prompt: 'Task 1' }),
  generateText({ model: openai('gpt-4'), prompt: 'Task 2' }),
  generateText({ model: openai('gpt-4'), prompt: 'Task 3' })
]);
```

**Go:**
```go
var wg sync.WaitGroup
results := make([]*ai.GenerateTextResult, 3)
errors := make([]error, 3)

prompts := []string{"Task 1", "Task 2", "Task 3"}

for i, prompt := range prompts {
    wg.Add(1)
    go func(idx int, p string) {
        defer wg.Done()
        results[idx], errors[idx] = ai.GenerateText(ctx, ai.GenerateTextOptions{
            Model:  model,
            Prompt: p,
        })
    }(i, prompt)
}

wg.Wait()

// Check all errors
for i, err := range errors {
    if err != nil {
        log.Printf("Request %d failed: %v", i, err)
    }
}
```

**Key Differences:**
- Go uses goroutines and `sync.WaitGroup`
- TypeScript uses `Promise.all()`
- Go requires explicit synchronization
- TypeScript is more concise for parallel async operations

---

## Tool Definitions

### Basic Tool

**TypeScript:**
```typescript
import { tool } from 'ai';
import { z } from 'zod';

const weatherTool = tool({
  description: 'Get weather for a location',
  parameters: z.object({
    location: z.string().describe('City name')
  }),
  execute: async ({ location }) => {
    const weather = await fetchWeather(location);
    return weather;
  }
});

const result = await generateText({
  model: openai('gpt-4'),
  prompt: "What's the weather in SF?",
  tools: { weather: weatherTool }
});
```

**Go:**
```go
import "github.com/digitallysavvy/go-ai/pkg/provider/types"

weatherTool := types.Tool{
    Name:        "get_weather",
    Description: "Get weather for a location",
    Parameters: map[string]interface{}{
        "type": "object",
        "properties": map[string]interface{}{
            "location": map[string]interface{}{
                "type":        "string",
                "description": "City name",
            },
        },
        "required": []string{"location"},
    },
    Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
        location := params["location"].(string)
        weather, err := fetchWeather(ctx, location)
        if err != nil {
            return nil, err
        }
        return weather, nil
    },
}

maxSteps := 5
result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:    model,
    Prompt:   "What's the weather in SF?",
    Tools:    []types.Tool{weatherTool},
    MaxSteps: &maxSteps,
})
```

**Key Differences:**
- TypeScript uses Zod schemas, Go uses JSON Schema maps
- Go tools in a slice (`[]types.Tool`), TypeScript in object
- Go requires `Name` field, TypeScript infers from object key
- Go execute takes `context.Context` as first parameter
- TypeScript has automatic type inference from schema
- Go requires manual type assertions from `params`

---

## Agent Patterns

### Basic Agent

**TypeScript:**
```typescript
import { Agent } from 'ai';

const agent = new Agent({
  model: openai('gpt-4'),
  system: 'You are a helpful assistant',
  tools: { calculator, weather },
  maxSteps: 5
});

const result = await agent.execute('What is 2+2 and the weather in NYC?');
console.log(result.text);
```

**Go:**
```go
import "github.com/digitallysavvy/go-ai/pkg/agent"

agentInstance := agent.NewToolLoopAgent(agent.AgentConfig{
    Model:    model,
    System:   "You are a helpful assistant",
    Tools:    []types.Tool{calculatorTool, weatherTool},
    MaxSteps: 5,
})

result, err := agentInstance.Execute(ctx, "What is 2+2 and the weather in NYC?")
if err != nil {
    log.Fatal(err)
}

fmt.Println(result.Text)
```

**Key Differences:**
- TypeScript uses `new Agent()`, Go uses factory function
- Go uses `NewToolLoopAgent` constructor pattern
- Go agent requires context in `Execute()`
- TypeScript tools in object, Go in slice

### Agent with Callbacks

**TypeScript:**
```typescript
const agent = new Agent({
  model: openai('gpt-4'),
  tools: { calculator },
  onStepFinish: (step) => {
    console.log(`Step ${step.stepNumber}: ${step.text}`);
  }
});
```

**Go:**
```go
agentInstance := agent.NewToolLoopAgent(agent.AgentConfig{
    Model: model,
    Tools: []types.Tool{calculatorTool},
    OnStepFinish: func(step types.StepResult) {
        fmt.Printf("Step: %s\n", step.Text)
    },
})
```

**Key Differences:**
- Both support callbacks
- Go callbacks are function types in config
- TypeScript callbacks get more metadata
- Go callbacks use struct receivers

---

## Middleware Patterns

### Logging Middleware

**TypeScript:**
```typescript
import { wrapLanguageModel } from 'ai';

const wrappedModel = wrapLanguageModel({
  model: openai('gpt-4'),
  middleware: {
    transformParams: async ({ params }) => {
      console.log('Request:', params);
      return params;
    },
    wrapResult: async ({ result }) => {
      console.log('Response:', result.text);
      return result;
    }
  }
});
```

**Go:**
```go
import "github.com/digitallysavvy/go-ai/pkg/middleware"

loggingMiddleware := &middleware.LanguageModelMiddleware{
    SpecificationVersion: "v3",
    TransformParams: func(ctx context.Context, callType string, params *provider.GenerateOptions, model provider.LanguageModel) (*provider.GenerateOptions, error) {
        log.Printf("Request: %+v", params)
        return params, nil
    },
    WrapResult: func(ctx context.Context, result *provider.GenerateResult) (*provider.GenerateResult, error) {
        log.Printf("Response: %s", result.Text)
        return result, nil
    },
}

wrappedModel := middleware.WrapLanguageModel(
    model,
    []*middleware.LanguageModelMiddleware{loggingMiddleware},
    nil, nil,
)
```

**Key Differences:**
- Go middleware is more verbose (struct vs object literal)
- Go requires specification version
- TypeScript uses object methods, Go uses function fields
- Go middleware uses pointers for efficiency

---

## HTTP Server Patterns

### Express vs Gin

**TypeScript (Express):**
```typescript
import express from 'express';
import { streamText } from 'ai';
import { openai } from '@ai-sdk/openai';

const app = express();
app.use(express.json());

app.post('/api/chat', async (req, res) => {
  const { messages } = req.body;

  const result = streamText({
    model: openai('gpt-4'),
    messages
  });

  result.pipeDataStreamToResponse(res);
});

app.listen(3000);
```

**Go (Gin):**
```go
import (
    "github.com/gin-gonic/gin"
    "github.com/digitallysavvy/go-ai/pkg/ai"
    "github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
    r := gin.Default()

    provider := openai.New(openai.Config{
        APIKey: os.Getenv("OPENAI_API_KEY"),
    })
    model, _ := provider.LanguageModel("gpt-4")

    r.POST("/api/chat", func(c *gin.Context) {
        var req struct {
            Messages []types.Message `json:"messages"`
        }
        if err := c.BindJSON(&req); err != nil {
            c.JSON(400, gin.H{"error": err.Error()})
            return
        }

        stream, err := ai.StreamText(c.Request.Context(), ai.StreamTextOptions{
            Model:    model,
            Messages: req.Messages,
        })
        if err != nil {
            c.JSON(500, gin.H{"error": err.Error()})
            return
        }

        c.Header("Content-Type", "text/event-stream")
        c.Header("Cache-Control", "no-cache")
        c.Header("Connection", "keep-alive")

        for chunk := range stream.Chunks() {
            if chunk.Type == provider.ChunkTypeText {
                fmt.Fprintf(c.Writer, "data: %s\n\n", chunk.Text)
                c.Writer.(http.Flusher).Flush()
            }
        }
    })

    r.Run(":3000")
}
```

**Key Differences:**
- Go requires explicit JSON struct definition
- Go needs manual SSE header setup
- Go requires explicit flush calls
- TypeScript has helper methods like `pipeDataStreamToResponse()`
- Go is more explicit, TypeScript is more magical

---

## Common Pitfalls

### 1. Forgetting Context

❌ **Wrong:**
```go
result, err := ai.GenerateText(ai.GenerateTextOptions{...})
```

✅ **Correct:**
```go
result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{...})
```

### 2. Not Checking Stream Errors

❌ **Wrong:**
```go
for chunk := range stream.Chunks() {
    fmt.Print(chunk.Text)
}
// Forgot to check stream.Err()
```

✅ **Correct:**
```go
for chunk := range stream.Chunks() {
    if chunk.Type == provider.ChunkTypeText {
        fmt.Print(chunk.Text)
    }
}
if err := stream.Err(); err != nil {
    log.Printf("Stream error: %v", err)
}
```

### 3. Incorrect Message Content Structure

❌ **Wrong:**
```go
messages := []types.Message{
    {Role: types.RoleUser, Content: "Hello"}, // Wrong: Content is not string
}
```

✅ **Correct:**
```go
messages := []types.Message{
    {
        Role: types.RoleUser,
        Content: []types.ContentPart{
            types.TextContent{Text: "Hello"},
        },
    },
}
```

### 4. Tools as Map Instead of Slice

❌ **Wrong:**
```go
Tools: map[string]types.Tool{
    "weather": weatherTool,
}
```

✅ **Correct:**
```go
Tools: []types.Tool{weatherTool}
```

### 5. Missing Tool Name

❌ **Wrong:**
```go
weatherTool := types.Tool{
    // Missing Name field
    Description: "Get weather",
    Parameters:  params,
    Execute:     executeFunc,
}
```

✅ **Correct:**
```go
weatherTool := types.Tool{
    Name:        "get_weather",  // Must have Name!
    Description: "Get weather",
    Parameters:  params,
    Execute:     executeFunc,
}
```

### 6. Wrong Execute Signature

❌ **Wrong:**
```go
Execute: func(params map[string]interface{}) (interface{}, error) {
    // Missing context.Context parameter
}
```

✅ **Correct:**
```go
Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
    // Context is first parameter
}
```

### 7. Not Using Pointer for Optional Fields

❌ **Wrong:**
```go
MaxSteps: 5  // Should be pointer for optional field
```

✅ **Correct:**
```go
maxSteps := 5
MaxSteps: &maxSteps
```

### 8. Forgetting Type Assertions

❌ **Wrong:**
```go
location := params["location"]  // interface{}, not string
weather := fetchWeather(location)
```

✅ **Correct:**
```go
location, ok := params["location"].(string)
if !ok {
    return nil, fmt.Errorf("location must be string")
}
weather := fetchWeather(location)
```

---

## Quick Reference Table

| Feature | TypeScript | Go |
|---------|-----------|-----|
| **Import** | `import { generateText } from 'ai'` | `import "github.com/digitallysavvy/go-ai/pkg/ai"` |
| **Model** | `openai('gpt-4')` | `provider.LanguageModel("gpt-4")` |
| **Generate** | `await generateText({...})` | `ai.GenerateText(ctx, ai.GenerateTextOptions{...})` |
| **Stream** | `for await (const chunk of stream)` | `for chunk := range stream.Chunks()` |
| **Messages** | `{ role: 'user', content: 'hi' }` | `types.Message{Role: types.RoleUser, Content: []types.ContentPart{...}}` |
| **Tools** | `tools: { weather: tool }` | `Tools: []types.Tool{weatherTool}` |
| **Execute** | `execute: async ({ location })` | `Execute: func(ctx context.Context, params map[string]interface{})` |
| **Agent** | `new Agent({...})` | `agent.NewToolLoopAgent(agent.AgentConfig{...})` |
| **Error** | `try { ... } catch (e)` | `result, err := ...; if err != nil { ... }` |
| **Cancel** | `abortSignal` | `context.WithCancel()` |

---

## Summary

When translating TypeScript to Go:

1. **Add Context**: Every API call needs `context.Context`
2. **Handle Errors**: Check `err` after every function call
3. **Use Structs**: Options are typed structs, not object literals
4. **Channels for Streams**: Use `range` over channels instead of async iterators
5. **Type Assertions**: Explicitly cast `interface{}` to concrete types
6. **Pointers for Optionals**: Use pointers (`*int`) for optional parameters
7. **Explicit Headers**: Manually set SSE/streaming headers
8. **Goroutines**: Use goroutines and WaitGroup instead of Promise.all
9. **Content Parts**: Messages have `[]ContentPart`, not string content
10. **Tools Slice**: Tools in slice, not map; include Name field

**Remember:** Go favors explicitness over magic. What's automatic in TypeScript often requires manual setup in Go.

---

**Reference Examples:**
- Go examples: `/Users/arlene/Dev/side-projects/go-ai/go-ai/examples/`
- TypeScript examples: `/Users/arlene/Dev/side-projects/go-ai/ai/examples/`
- Go SDK packages: `/Users/arlene/Dev/side-projects/go-ai/go-ai/pkg/`

# Go AI SDK Examples Roadmap

**Status:** In Progress
**Last Updated:** 2024-12-08
**Purpose:** Achieve parity with TypeScript AI SDK server-side examples

## Executive Summary

The Go AI SDK currently has **4 examples** while the TypeScript AI SDK has **23 example directories** with over **600 example files** in the ai-core directory alone. This document outlines the gap and provides a roadmap for achieving server-side parity.

## Current State

### Existing Go Examples (4)

Located in `/examples/`:

1. **text-generation/** - Basic text generation, streaming, tool calling
2. **cli-chat/** - Interactive terminal chat with conversation history
3. **middleware/** - Default settings and custom middleware patterns
4. **comprehensive/** - Multi-provider support, embeddings, agents

**Status:** ✅ All examples compile and work correctly

### TypeScript Reference Examples

Located in TypeScript SDK at `/Users/arlene/Dev/side-projects/go-ai/ai/examples/`:

- **23 total example directories**
- **600+ example files** in ai-core alone
- Covers 30+ AI providers
- Includes HTTP frameworks, MCP, agents, advanced patterns

## Gap Analysis

### Category 1: HTTP Server Examples ⚠️ CRITICAL GAP

TypeScript has 5 HTTP framework examples. **Go has 0.**

| TypeScript Example | Go Equivalent Needed | Priority |
|-------------------|---------------------|----------|
| `node-http-server/` | `http-server/` (net/http) | P0 |
| `express/` | `gin-server/` or `echo-server/` | P0 |
| `fastify/` | `fiber-server/` | P1 |
| `hono/` | `chi-server/` | P2 |
| `nestjs/` | N/A (Go uses simpler patterns) | P3 |

**Why Critical:** Most users need HTTP endpoints, not CLI tools.

### Category 2: AI Core Features ⚠️ MAJOR GAP

TypeScript has **ai-core/** with 22 subdirectories. **Go has partial coverage.**

| Feature Area | TypeScript Files | Go Status | Priority |
|--------------|------------------|-----------|----------|
| generate-text | 274 files | ✅ Basic only | P0 |
| stream-text | 246 files | ✅ Basic only | P0 |
| generate-object | 55 files | ❌ Missing | P0 |
| stream-object | 38 files | ❌ Missing | P1 |
| generate-image | 33 files | ❌ Missing | P1 |
| generate-speech | 30 files | ❌ Missing | P2 |
| transcribe | 31 files | ❌ Missing | P2 |
| embed/embed-many | 26 files | ✅ Basic only | P1 |
| agent | 15 files | ✅ Basic only | P0 |
| rerank | 9 files | ❌ Missing | P2 |
| middleware | 16 files | ⚠️ Minimal | P1 |
| telemetry | 7 files | ❌ Missing | P2 |
| complex/math-agent | 1 file | ❌ Missing | P1 |
| complex/semantic-router | 1 file | ❌ Missing | P2 |
| e2e tests | 20 files | ❌ Missing | P2 |
| benchmarks | 2 files | ❌ Missing | P3 |

### Category 3: MCP (Model Context Protocol) ⚠️ MISSING

TypeScript has extensive MCP examples. **Go has none.**

| MCP Feature | TypeScript Status | Go Status | Priority |
|-------------|------------------|-----------|----------|
| stdio transport | ✅ Yes | ❌ No | P1 |
| HTTP transport | ✅ Yes | ❌ No | P1 |
| SSE transport | ✅ Yes (legacy) | ❌ No | P2 |
| Authentication | ✅ Yes | ❌ No | P2 |
| Prompts | ✅ Yes | ❌ No | P2 |
| Resources | ✅ Yes | ❌ No | P2 |
| Elicitation | ✅ Yes | ❌ No | P3 |

### Category 4: Provider-Specific Examples ⚠️ MAJOR GAP

TypeScript has provider-specific examples for 30+ providers. **Go has generic multi-provider only.**

| Provider Category | TypeScript Coverage | Go Coverage | Priority |
|------------------|-------------------|-------------|----------|
| OpenAI (reasoning, structured outputs, assistants) | ✅ Extensive | ❌ Generic | P0 |
| Anthropic (cache, computer use, PDF, code exec) | ✅ Extensive | ❌ Generic | P0 |
| Google (thinking mode, audio, caching) | ✅ Extensive | ❌ Generic | P1 |
| Azure (code interpreter, file search, web search) | ✅ Extensive | ❌ Generic | P1 |
| Amazon Bedrock (guardrails, reasoning, Nova) | ✅ Extensive | ❌ Generic | P1 |
| Other providers (20+ providers) | ✅ Yes | ❌ Generic | P2 |

## Recommended Implementation Plan

### Phase 1: HTTP Server Fundamentals (Week 1-2)

**Goal:** Enable users to build HTTP APIs with streaming

Create these examples:

```
examples/
├── http-server/              # Priority: P0
│   ├── main.go              # Basic net/http server
│   ├── README.md
│   └── handlers/
│       ├── stream.go        # Streaming endpoint
│       ├── generate.go      # Basic generation
│       └── tools.go         # Tool calling
│
├── gin-server/              # Priority: P0
│   ├── main.go              # Gin framework
│   ├── README.md
│   └── routes/
│       ├── chat.go          # Chat endpoint
│       ├── stream.go        # SSE streaming
│       └── agent.go         # Agent endpoint
│
└── echo-server/             # Priority: P1
    ├── main.go              # Echo framework
    ├── README.md
    └── handlers/
        ├── generation.go
        └── middleware.go
```

**Key Features to Demonstrate:**
- Server-Sent Events (SSE) streaming
- Chat completion endpoints
- Agent-based endpoints
- Error handling
- CORS configuration
- Request validation

**Implementation Notes:**
```go
// Example SSE streaming pattern
func streamHandler(c *gin.Context) {
    c.Header("Content-Type", "text/event-stream")
    c.Header("Cache-Control", "no-cache")
    c.Header("Connection", "keep-alive")

    stream, err := ai.StreamText(ctx, ai.StreamTextOptions{
        Model:  model,
        Prompt: req.Prompt,
    })

    for chunk := range stream.Chunks() {
        if chunk.Type == provider.ChunkTypeText {
            fmt.Fprintf(c.Writer, "data: %s\n\n", chunk.Text)
            c.Writer.(http.Flusher).Flush()
        }
    }
}
```

### Phase 2: Structured Output & Objects (Week 2-3)

**Goal:** Demonstrate type-safe structured generation

Create these examples:

```
examples/
├── generate-object/         # Priority: P0
│   ├── basic/
│   │   └── main.go         # Simple struct generation
│   ├── validation/
│   │   └── main.go         # With JSON schema validation
│   ├── complex/
│   │   └── main.go         # Nested structures
│   └── README.md
│
└── stream-object/           # Priority: P1
    ├── main.go              # Streaming structured output
    └── README.md
```

**Example Patterns:**
```go
// Define output structure
type Recipe struct {
    Name        string   `json:"name"`
    Ingredients []string `json:"ingredients"`
    Steps       []string `json:"steps"`
    PrepTime    int      `json:"prepTime"`
}

// Generate with schema
schema := schema.NewSimpleJSONSchema(map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "name": map[string]interface{}{"type": "string"},
        "ingredients": map[string]interface{}{
            "type": "array",
            "items": map[string]interface{}{"type": "string"},
        },
        // ... more fields
    },
    "required": []string{"name", "ingredients", "steps"},
})

result, err := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
    Model:  model,
    Prompt: "Generate a recipe for chocolate chip cookies",
    Schema: schema,
})
```

### Phase 3: Provider-Specific Features (Week 3-4)

**Goal:** Show unique capabilities of major providers

Create these examples:

```
examples/
└── providers/
    ├── openai/              # Priority: P0
    │   ├── reasoning/
    │   │   └── main.go     # o1/o3 reasoning models
    │   ├── structured-outputs/
    │   │   └── main.go     # Native structured outputs
    │   ├── vision/
    │   │   └── main.go     # Image understanding
    │   └── README.md
    │
    ├── anthropic/           # Priority: P0
    │   ├── caching/
    │   │   └── main.go     # Prompt caching
    │   ├── extended-thinking/
    │   │   └── main.go     # Extended thinking mode
    │   ├── pdf-support/
    │   │   └── main.go     # PDF analysis
    │   └── README.md
    │
    ├── google/              # Priority: P1
    │   ├── thinking-mode/
    │   │   └── main.go     # Gemini thinking
    │   ├── multimodal/
    │   │   └── main.go     # Audio + video
    │   └── README.md
    │
    └── azure/               # Priority: P1
        ├── assistants/
        │   └── main.go      # Azure OpenAI assistants
        └── README.md
```

**OpenAI Reasoning Example:**
```go
// Using o1 reasoning models
result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:  openaiModel("o1-preview"),
    Prompt: "Solve this complex math problem step by step...",
    // o1 models don't support temperature/top_p
})

// Access reasoning tokens
fmt.Printf("Reasoning tokens: %d\n", result.Usage.ReasoningTokens)
```

**Anthropic Caching Example:**
```go
// Using prompt caching
result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model:  anthropicModel("claude-sonnet-4"),
    System: largeSystemPrompt, // This will be cached
    Messages: messages,
    // Anthropic-specific: enable caching
    Extra: map[string]interface{}{
        "cache_control": map[string]interface{}{
            "type": "ephemeral",
        },
    },
})
```

### Phase 4: Advanced Agent Patterns (Week 4-5)

**Goal:** Demonstrate complex agent architectures

Create these examples:

```
examples/
└── agents/
    ├── math-agent/          # Priority: P1
    │   ├── main.go         # Multi-tool math solver
    │   ├── tools.go        # Calculator, wolfram, etc.
    │   └── README.md
    │
    ├── web-search-agent/    # Priority: P1
    │   ├── main.go         # Agent with web search
    │   ├── search.go       # Search tool implementation
    │   └── README.md
    │
    ├── streaming-agent/     # Priority: P1
    │   └── main.go         # Agent with streaming output
    │
    ├── multi-agent/         # Priority: P2
    │   └── main.go         # Coordinated multi-agent system
    │
    └── supervisor-agent/    # Priority: P2
        └── main.go          # Agent that manages other agents
```

**Math Agent Example:**
```go
// Define specialized tools
calculatorTool := types.Tool{
    Name: "calculator",
    Description: "Performs basic arithmetic",
    Parameters: calculatorSchema,
    Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
        // Implementation
    },
}

wolframTool := types.Tool{
    Name: "wolfram",
    Description: "Solves complex mathematical equations",
    Parameters: wolframSchema,
    Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
        // Call Wolfram Alpha API
    },
}

// Create agent with multiple tools
agent := agent.NewToolLoopAgent(agent.AgentConfig{
    Model:    model,
    System:   "You are a mathematical reasoning assistant...",
    Tools:    []types.Tool{calculatorTool, wolframTool},
    MaxSteps: 10,
    OnStepFinish: func(step types.StepResult) {
        fmt.Printf("Step %d: %s\n", stepNum, step.Text)
    },
})
```

### Phase 5: Production Patterns (Week 5-6)

**Goal:** Show production-ready implementations

Create these examples:

```
examples/
├── middleware/
│   ├── logging/             # Priority: P1
│   │   └── main.go         # Request/response logging
│   ├── caching/             # Priority: P1
│   │   └── main.go         # Response caching
│   ├── rate-limiting/       # Priority: P1
│   │   └── main.go         # Token bucket rate limiting
│   ├── retry/               # Priority: P2
│   │   └── main.go         # Exponential backoff
│   └── telemetry/           # Priority: P2
│       └── main.go          # OpenTelemetry integration
│
├── testing/                 # Priority: P2
│   ├── unit/
│   │   └── main_test.go    # Unit testing patterns
│   ├── integration/
│   │   └── main_test.go    # Integration tests
│   └── mocks/
│       └── provider.go      # Mock providers
│
└── benchmarks/              # Priority: P3
    ├── throughput/
    │   └── main.go          # Throughput benchmarking
    └── latency/
        └── main.go          # Latency benchmarking
```

**Logging Middleware Example:**
```go
// Request logging middleware
func LoggingMiddleware() *middleware.LanguageModelMiddleware {
    return &middleware.LanguageModelMiddleware{
        SpecificationVersion: "v3",
        TransformParams: func(ctx context.Context, callType string, params *provider.GenerateOptions, model provider.LanguageModel) (*provider.GenerateOptions, error) {
            start := time.Now()

            log.Printf("[Request] Model: %s, Type: %s", model.ID(), callType)

            // Log after completion in response handler
            return params, nil
        },
        WrapResult: func(ctx context.Context, result *provider.GenerateResult) (*provider.GenerateResult, error) {
            duration := time.Since(ctx.Value("start_time").(time.Time))

            log.Printf("[Response] Tokens: %d, Duration: %s",
                result.Usage.TotalTokens,
                duration)

            return result, nil
        },
    }
}
```

**Rate Limiting Example:**
```go
// Token bucket rate limiter
type RateLimiter struct {
    tokensPerSecond int
    bucket          chan struct{}
}

func (rl *RateLimiter) Middleware() *middleware.LanguageModelMiddleware {
    return &middleware.LanguageModelMiddleware{
        SpecificationVersion: "v3",
        TransformParams: func(ctx context.Context, callType string, params *provider.GenerateOptions, model provider.LanguageModel) (*provider.GenerateOptions, error) {
            // Wait for available token
            select {
            case <-rl.bucket:
                return params, nil
            case <-ctx.Done():
                return nil, ctx.Err()
            }
        },
    }
}
```

### Phase 6: MCP Integration (Week 6-7)

**Goal:** Enable Model Context Protocol support

Create these examples:

```
examples/
└── mcp/
    ├── stdio/               # Priority: P1
    │   ├── server/
    │   │   └── main.go     # MCP stdio server
    │   ├── client/
    │   │   └── main.go     # MCP stdio client
    │   └── README.md
    │
    ├── http/                # Priority: P1
    │   ├── server/
    │   │   └── main.go     # MCP HTTP server
    │   ├── client/
    │   │   └── main.go     # MCP HTTP client
    │   └── README.md
    │
    ├── with-auth/           # Priority: P2
    │   └── main.go          # Authenticated MCP
    │
    └── tools/               # Priority: P2
        └── main.go          # MCP tool definitions
```

**MCP Server Example:**
```go
// MCP stdio server
type MCPServer struct {
    tools map[string]types.Tool
}

func (s *MCPServer) HandleRequest(req MCPRequest) (MCPResponse, error) {
    switch req.Method {
    case "tools/list":
        return s.listTools()
    case "tools/call":
        return s.callTool(req.Params)
    default:
        return nil, fmt.Errorf("unknown method: %s", req.Method)
    }
}

func main() {
    server := NewMCPServer()

    // Register tools
    server.RegisterTool(weatherTool)
    server.RegisterTool(calculatorTool)

    // Start stdio server
    scanner := bufio.NewScanner(os.Stdin)
    for scanner.Scan() {
        var req MCPRequest
        json.Unmarshal(scanner.Bytes(), &req)

        resp, err := server.HandleRequest(req)
        json.NewEncoder(os.Stdout).Encode(resp)
    }
}
```

### Phase 7: Additional Modalities (Week 7-8)

**Goal:** Support image, speech, and transcription

Create these examples:

```
examples/
├── image-generation/        # Priority: P1
│   ├── dalle/
│   │   └── main.go         # OpenAI DALL-E
│   ├── stable-diffusion/
│   │   └── main.go         # Stability AI
│   └── README.md
│
├── speech/                  # Priority: P2
│   ├── text-to-speech/
│   │   └── main.go         # OpenAI TTS
│   └── speech-to-text/
│       └── main.go          # Whisper transcription
│
└── multimodal/              # Priority: P2
    ├── vision/
    │   └── main.go          # Image understanding
    ├── audio/
    │   └── main.go          # Audio analysis
    └── video/
        └── main.go          # Video analysis (if supported)
```

## Implementation Guidelines

### Code Quality Standards

1. **All examples must:**
   - Compile without errors
   - Include comprehensive README.md
   - Have clear, commented code
   - Follow Go idioms and best practices
   - Include error handling
   - Show usage examples in README

2. **README.md template:**
```markdown
# [Example Name]

Brief description of what this example demonstrates.

## Features Demonstrated

- Feature 1
- Feature 2
- Feature 3

## Prerequisites

- Go 1.21 or higher
- [Required API keys]

## Setup

1. Set environment variables:
\`\`\`bash
export OPENAI_API_KEY=sk-...
\`\`\`

2. Run the example:
\`\`\`bash
go run main.go
\`\`\`

## What You'll Learn

- Learning objective 1
- Learning objective 2

## Expected Output

Description of what users should see.

## Code Highlights

Key code snippets with explanations.

## Next Steps

- Link to related examples
- Link to relevant documentation
```

3. **Code structure:**
   - Keep main.go focused on demonstration
   - Extract complex logic into separate files
   - Use meaningful variable names
   - Add comments for non-obvious code
   - Show both success and error cases

### Testing Requirements

Each example should be tested to ensure:

1. **Compilation:** `go build` succeeds
2. **Validation:** `go vet` passes with no warnings
3. **Formatting:** Code is `go fmt` formatted
4. **Dependencies:** `go mod tidy` has been run

**Test script:**
```bash
#!/bin/bash
# test-examples.sh

EXAMPLES_DIR="examples"

for dir in $(find $EXAMPLES_DIR -name "main.go" -exec dirname {} \;); do
    echo "Testing $dir..."

    cd "$dir" || exit 1

    # Build
    if ! go build -o /dev/null .; then
        echo "❌ Build failed: $dir"
        exit 1
    fi

    # Vet
    if ! go vet ./...; then
        echo "❌ Vet failed: $dir"
        exit 1
    fi

    echo "✅ $dir passed"
    cd - > /dev/null
done

echo "✅ All examples passed"
```

### Documentation Standards

1. **Main examples README** (`examples/README.md`) must:
   - List all examples with descriptions
   - Organize by category
   - Include learning path recommendations
   - Link to relevant documentation

2. **Individual example READMEs** must:
   - Be self-contained (assume no prior context)
   - Include working code snippets
   - Show expected output
   - Link to related examples

3. **Code comments** should:
   - Explain WHY, not WHAT
   - Highlight Go-specific patterns
   - Reference documentation when relevant
   - Include links to provider docs for provider-specific features

## Priority Matrix

### P0 - Critical (Complete in Weeks 1-2)
- [ ] http-server (net/http)
- [ ] gin-server (Gin framework)
- [ ] generate-object (structured output)
- [ ] providers/openai (reasoning, structured outputs)
- [ ] providers/anthropic (caching, extended thinking)

### P1 - High Priority (Complete in Weeks 3-5)
- [ ] echo-server (Echo framework)
- [ ] stream-object (streaming structured output)
- [ ] generate-image (image generation)
- [ ] agents/math-agent (complex agent)
- [ ] agents/web-search-agent (web search)
- [ ] agents/streaming-agent (streaming agent)
- [ ] middleware/logging (logging middleware)
- [ ] middleware/caching (caching middleware)
- [ ] middleware/rate-limiting (rate limiting)
- [ ] mcp/stdio (MCP stdio transport)
- [ ] mcp/http (MCP HTTP transport)
- [ ] providers/google (thinking mode)
- [ ] providers/azure (assistants)

### P2 - Medium Priority (Complete in Weeks 6-7)
- [ ] fiber-server (Fiber framework)
- [ ] generate-speech (text-to-speech)
- [ ] transcribe (speech-to-text)
- [ ] rerank (document reranking)
- [ ] agents/multi-agent (multi-agent system)
- [ ] middleware/retry (retry logic)
- [ ] middleware/telemetry (OpenTelemetry)
- [ ] testing/unit (unit tests)
- [ ] testing/integration (integration tests)
- [ ] mcp/with-auth (authenticated MCP)
- [ ] speech/text-to-speech
- [ ] speech/speech-to-text
- [ ] multimodal/vision

### P3 - Low Priority (Complete in Week 8+)
- [ ] chi-server (Chi framework)
- [ ] agents/supervisor-agent (supervisor pattern)
- [ ] benchmarks/throughput
- [ ] benchmarks/latency
- [ ] complex/semantic-router
- [ ] e2e tests
- [ ] multimodal/audio
- [ ] multimodal/video

## Success Metrics

The examples initiative will be considered successful when:

1. **Coverage:** At least 20 new examples created (from current 4 to 24+)
2. **Quality:** All examples compile, are well-documented, and follow Go idioms
3. **Parity:** Core use cases from TypeScript SDK are covered
4. **Adoption:** GitHub stars/forks increase, community feedback is positive
5. **Documentation:** Each example has comprehensive README with working code

## Resources

### TypeScript SDK Reference
- Path: `/Users/arlene/Dev/side-projects/go-ai/ai/examples/`
- Total examples: 23 directories, 600+ files
- Reference for API patterns and use cases

### Go SDK Codebase
- Path: `/Users/arlene/Dev/side-projects/go-ai/go-ai/`
- Current examples: `/examples/` (4 examples)
- SDK packages: `/pkg/ai/`, `/pkg/provider/`, `/pkg/agent/`

### External References
- [Vercel AI SDK Documentation](https://sdk.vercel.ai/docs)
- [Go AI SDK Documentation](../docs/)
- Provider documentation:
  - [OpenAI API](https://platform.openai.com/docs)
  - [Anthropic Claude](https://docs.anthropic.com)
  - [Google Gemini](https://ai.google.dev)

## Questions for Team Lead

Before starting implementation, please clarify:

1. **Priorities:** Should we focus on HTTP servers first, or provider examples?
2. **Frameworks:** Which Go HTTP frameworks should we prioritize? (Gin, Echo, Fiber, Chi, net/http)
3. **Providers:** Which providers are most important to users? (OpenAI, Anthropic, Google, Azure, Bedrock?)
4. **Testing:** Should examples include unit tests, or just working demonstrations?
5. **MCP:** Is Model Context Protocol support a priority?
6. **Timeline:** Is 8 weeks realistic for completing P0-P1 examples?

## Next Steps

1. **Review this document** and provide feedback
2. **Prioritize categories** based on user needs
3. **Assign examples** to team members
4. **Set up tracking** (GitHub issues/project board)
5. **Begin Phase 1** (HTTP server examples)

---

**Document Maintained By:** [Your Name]
**Last Review:** 2024-12-08
**Next Review:** Weekly during implementation

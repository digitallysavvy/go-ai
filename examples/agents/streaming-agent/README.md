# Streaming Agent Example

Demonstrates an AI agent that streams its responses in real-time while using multiple tools to complete complex tasks.

## Features

This example shows:

- ✅ Real-time streaming of agent responses
- ✅ Step-by-step progress visualization
- ✅ Multiple specialized tools (research, data analysis, code review)
- ✅ Tool call and result tracking
- ✅ Live reasoning display
- ✅ Token usage monitoring
- ✅ Formatted streaming output

## What Makes This Different

Unlike basic agents that return complete responses, streaming agents provide:

1. **Progressive Updates**: See results as they're generated
2. **Step Visibility**: Watch the agent think through each step
3. **Tool Transparency**: Observe which tools are called and when
4. **Better UX**: Users aren't waiting without feedback
5. **Real-time Processing**: Results appear as soon as available

## Prerequisites

- Go 1.21 or higher
- OpenAI API key

## Setup

```bash
export OPENAI_API_KEY=sk-...
go run main.go
```

## How It Works

The streaming agent:

1. Receives a complex query
2. Breaks it down into steps
3. Calls appropriate tools
4. Streams reasoning and results in real-time
5. Provides final synthesized answer

## Example Output

```
=== Streaming Agent with Real-time Updates ===

==================== Example 1: Research Task ====================
Query: Research the history of Go programming language and summarize key milestones

[Step 1]
🔧 Tool Calls:
   • research
     Query: Go programming language history

📊 Results:
   Found 5 findings on Go Programming Language

💭 Reasoning:
   I'll research the history of Go to gather key milestones...

[Streaming Response]
📝 Go (Golang) was created at Google in 2007 by Robert Griesemer, Rob Pike, and Ken Thompson.
   It was first released as open source in November 2009. The language reached version 1.0 in March 2012.
   Go is known for its simplicity, excellent concurrency support through goroutines, and fast compilation times.
   Today it's used by major companies including Google, Uber, Docker, and Kubernetes.

[Statistics]
   Steps: 2
   Tokens: 347 (input: 215, output: 132)
```

## Available Tools

### 1. Research Tool

Retrieves information on any topic with configurable depth:

```go
Parameters:
- query (string): Topic to research
- depth (string): "quick" | "detailed" | "comprehensive"

Returns:
- topic: Research subject
- findings: Array of key findings
- sources: Information sources
- timestamp: When research was conducted
```

**Example Use**: Historical research, fact-finding, topic exploration

### 2. Data Analysis Tool

Analyzes datasets and generates insights:

```go
Parameters:
- dataset (string): Dataset name or description
- metrics (array): Metrics to calculate

Returns:
- summary: Dataset overview
- insights: Key findings
- recommendations: Actionable suggestions
```

**Example Use**: Sales analysis, performance review, trend identification

### 3. Code Analyzer Tool

Reviews code for issues and improvements:

```go
Parameters:
- code (string): Code to analyze
- language (string): Programming language (default: "go")

Returns:
- issues: Array of identified issues
- summary: Issue counts by severity
- suggestions: Fix recommendations
```

**Example Use**: Code review, bug detection, security analysis

## Code Highlights

### Streaming Setup

```go
stream, err := ai.StreamText(ctx, ai.StreamTextOptions{
    Model:  model,
    Prompt: query,
    Tools:  tools,
    MaxSteps: &maxSteps,
    OnStepFinish: func(step types.StepResult) {
        // Display step information in real-time
        showToolCalls(step.ToolCalls)
        showToolResults(step.ToolResults)
        showReasoning(step.Text)
    },
})
```

### Real-time Output

```go
for chunk := range stream.Chunks() {
    if chunk.Type == provider.ChunkTypeText {
        fmt.Print(chunk.Text)

        // Format output for readability
        if needsNewline(chunk.Text) {
            fmt.Println()
        }
    }
}
```

### Step Visualization

```go
OnStepFinish: func(step types.StepResult) {
    fmt.Printf("\n[Step %d]\n", stepCount)

    // Show tool calls with arguments
    for _, tc := range step.ToolCalls {
        fmt.Printf("🔧 %s: %v\n", tc.ToolName, tc.Arguments)
    }

    // Show tool results
    for _, tr := range step.ToolResults {
        fmt.Printf("📊 %s\n", summarize(tr.Result))
    }
}
```

## Use Cases

### 1. Research Assistant

Stream research findings as they're discovered:

```go
query := "Research the latest developments in quantum computing"
// Agent streams: searches → gathers data → synthesizes → presents findings
```

### 2. Data Analysis Dashboard

Show analysis progress in real-time:

```go
query := "Analyze Q4 sales data and identify trends"
// Agent streams: loads data → calculates metrics → generates insights → creates report
```

### 3. Code Review Bot

Provide incremental code review feedback:

```go
query := "Review this authentication implementation for security issues"
// Agent streams: analyzes code → identifies issues → suggests fixes → provides summary
```

### 4. Interactive Tutor

Teach concepts step-by-step:

```go
query := "Explain how blockchain works"
// Agent streams: breaks down concepts → provides examples → answers questions
```

## Streaming vs Non-Streaming

| Feature | Non-Streaming | Streaming |
|---------|--------------|-----------|
| **User Feedback** | Wait for complete response | See progress immediately |
| **Perceived Speed** | Feels slower | Feels faster |
| **Error Handling** | Fail at the end | Fail early |
| **Transparency** | Black box | See reasoning |
| **User Experience** | Waiting spinner | Progressive content |

## Advanced Patterns

### Custom Stream Formatting

```go
textBuffer := ""
for chunk := range stream.Chunks() {
    textBuffer += chunk.Text

    // Format paragraphs
    if strings.Contains(textBuffer, "\n\n") {
        paragraphs := strings.Split(textBuffer, "\n\n")
        for _, p := range paragraphs[:len(paragraphs)-1] {
            fmt.Printf("  %s\n\n", p)
        }
        textBuffer = paragraphs[len(paragraphs)-1]
    }
}
```

### Progress Indicators

```go
totalSteps := 5
OnStepFinish: func(step types.StepResult) {
    currentStep++
    percentage := (currentStep * 100) / totalSteps
    fmt.Printf("[%d%%] Step %d/%d\n", percentage, currentStep, totalSteps)
}
```

### Token Streaming

```go
tokenCount := 0
for chunk := range stream.Chunks() {
    if chunk.Type == provider.ChunkTypeText {
        tokens := estimateTokens(chunk.Text)
        tokenCount += tokens

        if tokenCount%100 == 0 {
            fmt.Printf("\n[Tokens: %d]", tokenCount)
        }
    }
}
```

## Performance Considerations

### Buffering

Stream in chunks for better performance:

```go
buffer := make([]string, 0, 10)
for chunk := range stream.Chunks() {
    buffer = append(buffer, chunk.Text)

    if len(buffer) >= 10 {
        fmt.Print(strings.Join(buffer, ""))
        buffer = buffer[:0]
    }
}
```

### Flush Control

Control when output is flushed:

```go
var flusher http.Flusher
if f, ok := w.(http.Flusher); ok {
    flusher = f
}

for chunk := range stream.Chunks() {
    fmt.Fprint(w, chunk.Text)

    // Flush on sentence boundaries
    if strings.HasSuffix(chunk.Text, ". ") {
        flusher.Flush()
    }
}
```

## Testing

Run the example:

```bash
go run main.go
```

Expected output:
- Real-time step updates
- Tool calls and results
- Streaming final response
- Usage statistics

## Troubleshooting

### No Streaming Output

- Ensure you're iterating over `stream.Chunks()`
- Check that `ChunkTypeText` chunks are being handled
- Verify model supports streaming

### Slow Streaming

- Model may be generating slowly
- Network latency issues
- Check tool execution times

### Missing Steps

- Increase `MaxSteps` if agent needs more iterations
- Simplify query if too complex
- Check tool availability

## Next Steps

- Add WebSocket support for web clients
- Implement Server-Sent Events (SSE) endpoint
- Create interactive streaming UI
- Add streaming to HTTP endpoints
- Implement retry logic for failed steps

## Related Examples

- [math-agent](../math-agent) - Multi-tool math solver
- [web-search-agent](../web-search-agent) - Research agent
- [stream-object](../../stream-object) - Streaming structured output
- [http-server](../../http-server) - HTTP streaming endpoints

## Resources

- [Go AI SDK Documentation](../../../docs)
- [OpenAI Streaming Guide](https://platform.openai.com/docs/api-reference/streaming)
- [Server-Sent Events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events)

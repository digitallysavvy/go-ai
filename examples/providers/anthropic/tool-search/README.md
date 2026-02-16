# Tool Search Example

This example demonstrates how to use the Anthropic Tool Search tools (BM25 and Regex) to work with large tool catalogs.

## Features

- BM25 natural language tool search
- Regex pattern-based tool search
- Dynamic tool discovery and loading
- Deferred tool loading for large catalogs
- Automatic tool reference expansion

## Prerequisites

- Claude Opus 4.5 or Claude Sonnet 4.5 API access
- `ANTHROPIC_API_KEY` environment variable

## Usage

```bash
export ANTHROPIC_API_KEY=your_api_key_here
go run main.go
```

## What is Tool Search?

Tool search enables Claude to work with hundreds or thousands of tools by discovering and loading them on-demand. Instead of loading all tool definitions into the context window upfront, Claude:

1. Searches your tool catalog using natural language or regex patterns
2. Discovers relevant tools based on the search
3. Automatically loads only the tools it needs
4. Uses the discovered tools to complete tasks

This dramatically reduces context usage and allows scaling to massive tool catalogs.

## Tool Search Methods

### 1. BM25 Tool Search (Natural Language)

Uses BM25 algorithm for text-based relevance ranking.

```go
toolSearch := tools.ToolSearchBm2520251119()
```

**Query Examples:**
- "tools for weather data"
- "database query tools"
- "file operations"
- "API integrations for Slack"

**Best for:** Natural language queries, semantic search, user-facing applications

### 2. Regex Tool Search (Pattern Matching)

Uses Python re.search() syntax for pattern matching.

```go
toolSearch := tools.ToolSearchRegex20251119()
```

**Pattern Examples:**
- `"weather"` - matches tool names containing "weather"
- `"get_.*_data"` - matches get_user_data, get_weather_data, etc.
- `"database.*query|query.*database"` - OR patterns
- `"(?i)slack"` - case-insensitive search
- `"^create_"` - tools starting with "create_"

**Best for:** Structured tool naming, precise pattern matching, programmatic discovery

## How to Use Tool Search

### Step 1: Create Tool Search Tool

```go
// For BM25 (natural language)
toolSearchBM25 := tools.ToolSearchBm2520251119()

// For Regex (pattern matching)
toolSearchRegex := tools.ToolSearchRegex20251119()
```

### Step 2: Create Your Tool Catalog

```go
// Create all your tools
tools := map[string]ai.Tool{
    "get_weather": weatherTool,
    "get_user_data": userTool,
    "database_query": dbTool,
    // ... hundreds more tools
}
```

### Step 3: Mark Tools for Deferred Loading

When using tool search, mark other tools for deferred loading using provider options:

```go
// In the TypeScript SDK, this would be:
// providerOptions: { anthropic: { deferLoading: true } }

// In Go, this is handled automatically when using tool search
// Tools are loaded on-demand based on search results
```

### Step 4: Combine Tool Search with Your Catalog

```go
allTools := map[string]ai.Tool{
    "toolSearch": toolSearchBM25,
    // Add all your deferred tools
}
for name, tool := range yourToolCatalog {
    allTools[name] = tool
}
```

### Step 5: Use with GenerateText

```go
result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
    Model: model,
    Messages: []ai.Message{
        {
            Role: ai.RoleUser,
            Content: ai.MessageContent{
                Text: stringPtr("Get weather for San Francisco"),
            },
        },
    },
    Tools: allTools,
    MaxSteps: intPtr(10), // Allow multiple steps for search + use
})
```

## How It Works

1. **User Query**: "Get weather for San Francisco"
2. **Claude Uses Tool Search**: Searches for weather-related tools
3. **API Returns tool_reference Objects**: References to discovered tools
4. **API Expands References**: Full tool definitions are loaded
5. **Claude Uses Discovered Tool**: Calls the weather tool
6. **Result Returned**: Weather data for San Francisco

## Output Format

Tool search returns `tool_reference` objects:

```json
[
  {
    "type": "tool_reference",
    "toolName": "get_weather_forecast"
  },
  {
    "type": "tool_reference",
    "toolName": "get_current_weather"
  }
]
```

The API automatically expands these references into full tool definitions.

## Best Practices

### Tool Naming Conventions
- Use consistent naming patterns (e.g., `verb_object`)
- Include descriptive keywords in tool names
- Use underscores for multi-word names

### Tool Descriptions
- Write clear, searchable descriptions
- Include relevant keywords
- Describe what the tool does, not how

### Search Strategy
- Use BM25 for user-facing natural language queries
- Use Regex for programmatic discovery
- Consider hybrid approaches for complex scenarios

### Performance
- Keep tool descriptions concise
- Use appropriate search limits
- Monitor context usage

## Important Notes

- Tool search tools must NOT have `deferLoading: true`
- Other tools should be marked for deferred loading
- The API handles tool expansion automatically
- Supports deferred results for multi-turn discovery
- Maximum regex pattern length: 200 characters

## Scaling to Large Catalogs

Tool search is designed for large-scale applications:

- ✅ Hundreds of tools: Easy
- ✅ Thousands of tools: Recommended
- ✅ Tens of thousands: Supported

Without tool search, context limits restrict you to ~50-100 tools. With tool search, you can scale to massive catalogs.

## Error Handling

```go
result, err := ai.GenerateText(ctx, options)
if err != nil {
    log.Printf("Error: %v", err)
    return
}

// Check if tool search was used
for _, toolCall := range result.ToolCalls {
    if toolCall.ToolName == "anthropic.tool_search_bm25_20251119" ||
       toolCall.ToolName == "anthropic.tool_search_regex_20251119" {
        fmt.Println("Tool search was used!")
    }
}
```

## Example Use Cases

1. **Customer Support**: Search for relevant support tools based on user queries
2. **DevOps**: Discover infrastructure tools by service name
3. **Data Analysis**: Find data processing tools by data type
4. **API Integration**: Locate API clients by service name
5. **Code Generation**: Find code templates and generators

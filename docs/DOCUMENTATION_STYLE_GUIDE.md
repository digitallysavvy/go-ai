# Documentation Style Guide

This guide establishes standards for Go-AI SDK documentation to ensure consistency, clarity, and maintainability across all documentation files.

## File Naming Conventions

### General Rules
- Use lowercase with hyphens for separation: `error-handling.mdx`
- Use descriptive, specific names: `streaming-responses.mdx` not `streaming.mdx`
- Prefix numbered sections: `01-getting-started.mdx`, `02-configuration.mdx`
- Use `.mdx` extension for all documentation files
- Avoid abbreviations unless widely recognized (API, SDK, HTTP)

### Directory Structure
```
docs/
├── 01-getting-started/
│   ├── installation.mdx
│   └── quick-start.mdx
├── 02-core-concepts/
│   ├── client-configuration.mdx
│   └── message-types.mdx
├── 03-providers/
│   ├── anthropic.mdx
│   └── openai.mdx
└── 07-reference/
    ├── api/
    └── types/
```

### Naming Patterns by Type
- **Guides**: `{topic}.mdx` (e.g., `streaming-responses.mdx`)
- **API Reference**: `{package-or-type}.mdx` (e.g., `client.mdx`)
- **Provider Docs**: `{provider-name}.mdx` (e.g., `anthropic.mdx`)
- **Troubleshooting**: `{issue-category}.mdx` (e.g., `common-errors.mdx`)

## Markdown Formatting Standards

### Headings
- Use ATX-style headings (`#` syntax, not underlines)
- One H1 (`#`) per file, matching the page title
- Maintain heading hierarchy (don't skip levels)
- Use sentence case for headings: "Getting started" not "Getting Started"
- Add blank line before and after headings

```markdown
# Main title

## Section heading

### Subsection heading

Content here.

## Next section
```

### Lists
- Use `-` for unordered lists (consistent with Go community)
- Use `1.` for ordered lists (numbers auto-increment in rendering)
- Indent nested lists with 2 spaces
- Add blank line before and after lists
- Use parallel structure for list items

```markdown
Prerequisites:

- Go 1.21 or later
- Valid API key
- Internet connection

Steps:

1. Install the package
2. Configure your client
3. Make your first request
```

### Code Blocks
- Always specify language for syntax highlighting
- Use `go` for Go code, `bash` for shell commands, `json` for JSON
- Include descriptive comments in code examples
- Keep examples self-contained and runnable
- Show imports when relevant

````markdown
```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/teilomillet/go-ai"
)

func main() {
    client := gai.NewClient("your-api-key")
    // ... rest of example
}
```
````

### Links
- Use descriptive link text: `[error handling guide](error-handling.mdx)` not `[click here](error-handling.mdx)`
- Use relative paths for internal links: `../providers/anthropic.mdx`
- Include `.mdx` extension in links
- Verify links exist before committing
- Use reference-style links for repeated URLs

```markdown
For more details, see the [streaming guide](streaming-responses.mdx).

Check the [Anthropic provider documentation](../providers/anthropic.mdx) for specific options.
```

### Emphasis
- Use **bold** for UI elements, field names, and emphasis
- Use *italic* for introducing new terms
- Use `code` for inline code, variables, and paths
- Don't overuse emphasis - it reduces impact

```markdown
Set the **Temperature** field to `0.7` for more creative responses.

The *context window* determines how much text the model can process.
```

## Code Example Requirements

### Completeness
Every code example must:
- Be syntactically valid Go code
- Include all necessary imports
- Compile without errors
- Run successfully (unless explicitly demonstrating an error)
- Include error handling (unless focusing on other aspects)

### Example Structure
```go
package main

import (
    // Standard library imports first
    "context"
    "fmt"
    "log"

    // Third-party imports second, grouped by domain
    "github.com/teilomillet/go-ai"
    "github.com/teilomillet/go-ai/providers/anthropic"
)

func main() {
    // 1. Setup/configuration
    client := gai.NewClient(
        "your-api-key",
        gai.WithProvider(anthropic.NewProvider()),
    )

    // 2. Main operation
    response, err := client.Generate(context.Background(), &gai.Request{
        Messages: []gai.Message{
            {Role: "user", Content: "Hello!"},
        },
    })
    if err != nil {
        log.Fatalf("Error: %v", err)
    }

    // 3. Result handling/output
    fmt.Println(response.Content)
}
```

### Error Handling in Examples
- Always check errors in production-ready examples
- Use `log.Fatalf()` for simplicity in short examples
- Show proper error handling in comprehensive guides
- Explain error scenarios when relevant

```go
// Simple example
response, err := client.Generate(ctx, request)
if err != nil {
    log.Fatalf("Generation failed: %v", err)
}

// Comprehensive example with error handling
response, err := client.Generate(ctx, request)
if err != nil {
    // Check for specific error types
    if errors.Is(err, gai.ErrRateLimited) {
        log.Println("Rate limited, retrying...")
        // Retry logic
    } else {
        return fmt.Errorf("generation failed: %w", err)
    }
}
```

### Placeholder Values
- Use clear placeholder format: `your-api-key`, `your-model-name`
- Include comments explaining what to replace
- Provide example values when helpful

```go
// Replace with your actual API key from https://console.anthropic.com
client := gai.NewClient("your-api-key")

// Use a supported model name (e.g., claude-3-5-sonnet-20241022)
request.Model = "your-model-name"
```

### Comments
- Explain *why*, not *what* (code shows what)
- Use inline comments for non-obvious logic
- Use block comments for section explanations
- Keep comments concise and clear

```go
// Configure aggressive caching to reduce API costs
client := gai.NewClient(
    apiKey,
    gai.WithCaching(true),
    gai.WithCacheTTL(24 * time.Hour), // Cache for 24 hours
)
```

## Cross-Reference Guidelines

### When to Cross-Reference
- Link to related concepts on first mention
- Reference API documentation from guides
- Point to troubleshooting for common issues
- Link to prerequisites in getting started sections

### Cross-Reference Format
```markdown
## Streaming responses

This feature requires understanding [message types](../core-concepts/message-types.mdx).

For configuration options, see the [Client API reference](../reference/api/client.mdx).

If you encounter timeout errors, check the [troubleshooting guide](../troubleshooting/timeouts.mdx).
```

### Reference Patterns
- **Prerequisites**: "Before proceeding, ensure you understand..."
- **Related topics**: "See also:"
- **Deep dives**: "For more details, see..."
- **API docs**: "API reference: [Type Name](path)"

### Bidirectional Linking
When creating new documentation:
1. Add links from new page to existing related pages
2. Add links from existing pages back to new page
3. Update overview/index pages with new content
4. Add to navigation if applicable

## Best Practices Template

Every documentation page should follow this structure:

```markdown
# Page Title

Brief 1-2 sentence description of what this page covers.

## Overview

Longer introduction explaining:
- What the feature/concept is
- Why it's useful
- When to use it

## Prerequisites

- Required knowledge/setup
- Links to prerequisite documentation

## Basic usage

Simple, complete example demonstrating the most common use case.

```go
// Code example here
```

## Advanced usage

### Subsection 1

Explanation and example.

### Subsection 2

Explanation and example.

## Configuration options

Table or list of available options with descriptions.

| Option | Type | Description | Default |
|--------|------|-------------|---------|
| Name   | Type | What it does | Value |

## Best practices

- Recommendation 1
- Recommendation 2
- Recommendation 3

## Common pitfalls

Issues users frequently encounter and how to avoid them.

## Troubleshooting

Common problems and solutions.

## Related documentation

- [Related Topic 1](link)
- [Related Topic 2](link)

## API reference

- [Type Name](../reference/api/type.mdx)
- [Function Name](../reference/api/function.mdx)
```

## Error Handling Requirements

### Documenting Error Cases
Every feature that can fail must document:
- What errors can occur
- Why they occur
- How to handle them
- How to prevent them

### Error Documentation Format
```markdown
## Error handling

The following errors may occur:

### `ErrRateLimited`

**Cause**: Too many requests sent in a short time period.

**Handling**:
```go
if errors.Is(err, gai.ErrRateLimited) {
    time.Sleep(time.Second * 5)
    // Retry request
}
```

**Prevention**: Implement rate limiting in your application.
```

### Error Examples
Show both the error case and the handling:

```go
// This will fail if the API key is invalid
client := gai.NewClient("invalid-key")

response, err := client.Generate(ctx, request)
if err != nil {
    // Handle authentication errors
    if errors.Is(err, gai.ErrUnauthorized) {
        log.Println("Check your API key")
        return
    }
    log.Fatalf("Unexpected error: %v", err)
}
```

## Writing Style

### Voice and Tone
- Use second person ("you") to address the reader
- Be clear and direct, avoid unnecessary words
- Use active voice: "Configure the client" not "The client should be configured"
- Be helpful and encouraging, not condescending

### Technical Writing
- Define acronyms on first use: "Large Language Model (LLM)"
- Use precise technical terms consistently
- Explain concepts before using them
- Provide context for decisions and recommendations

### Examples
Good: "You can configure the timeout using the `WithTimeout()` option."
Bad: "The timeout can be configured by using the WithTimeout() option that is provided."

Good: "Set `Temperature` to 0 for deterministic outputs."
Bad: "If you want deterministic outputs, it would be recommended to consider setting the Temperature parameter to 0."

## Code Comments in Documentation

### Inline Documentation
```go
// Generate a completion using the Claude model
response, err := client.Generate(ctx, &gai.Request{
    Model: "claude-3-5-sonnet-20241022", // Recommended for most use cases
    Messages: []gai.Message{
        {
            Role:    "user",
            Content: "Explain quantum computing",
        },
    },
    Options: &gai.RequestOptions{
        Temperature: 0.7, // Higher = more creative, lower = more focused
        MaxTokens:   1000, // Limit response length
    },
})
```

### Step-by-Step Examples
```go
// Step 1: Initialize the client with your API key
client := gai.NewClient("your-api-key")

// Step 2: Create a request with system and user messages
request := &gai.Request{
    Messages: []gai.Message{
        {Role: "system", Content: "You are a helpful assistant."},
        {Role: "user", Content: "What is Go?"},
    },
}

// Step 3: Generate the response
response, err := client.Generate(context.Background(), request)

// Step 4: Handle any errors
if err != nil {
    log.Fatalf("Error: %v", err)
}

// Step 5: Use the response
fmt.Println(response.Content)
```

## Testing Examples

All code examples should:
1. Be extractable by `extract-examples.go` script
2. Compile successfully (or be marked as pseudo-code)
3. Run without errors (unless demonstrating error cases)
4. Be tested in CI/CD pipeline

Mark non-compilable examples:
````markdown
```go
// This is pseudo-code for illustration only
response := client.Generate(...)
process(response)
```
````

## Version-Specific Documentation

When documenting version-specific features:

```markdown
## Feature Name

> **Added in version 1.2.0**

Description of the feature.

> **Deprecated in version 2.0.0**: Use [new feature](link) instead.
```

## Accessibility

- Use descriptive link text (avoid "click here")
- Provide alt text for images: `![Client architecture diagram](arch.png)`
- Use semantic HTML in MDX components
- Ensure code examples are readable by screen readers
- Use tables for tabular data, not for layout

## Review Checklist

Before submitting documentation:

- [ ] File follows naming conventions
- [ ] Heading hierarchy is correct
- [ ] All code examples compile and run
- [ ] Links are valid and use relative paths
- [ ] Cross-references are bidirectional
- [ ] Error handling is documented
- [ ] Examples include necessary imports
- [ ] Writing is clear and concise
- [ ] Technical terms are defined
- [ ] Follows template structure
- [ ] Examples are tested by extraction script
- [ ] Related documentation is updated

## Additional Resources

- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Effective Go](https://golang.org/doc/effective_go)
- [Google Developer Documentation Style Guide](https://developers.google.com/style)
- [Write the Docs](https://www.writethedocs.org/guide/)

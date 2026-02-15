# Agent Skills

Agent skills are reusable behaviors that can be registered with agents to extend their capabilities. Skills provide a clean way to encapsulate common operations and share them across multiple agents.

## Overview

Skills in the Go-AI SDK allow you to:
- **Encapsulate reusable behaviors**: Define skills once and use them across multiple agents
- **Extend agent capabilities**: Add custom functionality beyond what tools provide
- **Organize agent logic**: Group related behaviors into named skills
- **Share instructions**: Include guidance on when and how to use each skill

## Basic Usage

### Creating a Skill

```go
skill := &agent.Skill{
    Name:        "weather",
    Description: "Get weather information for a location",
    Instructions: "Use this skill when the user asks about weather or climate",
    Handler: func(ctx context.Context, input string) (string, error) {
        // Implement weather fetching logic
        return fmt.Sprintf("Weather for %s: Sunny, 72°F", input), nil
    },
}
```

### Adding Skills to an Agent

```go
agentInstance := agent.NewToolLoopAgent(config)

if err := agentInstance.AddSkill(skill); err != nil {
    log.Fatalf("Failed to add skill: %v", err)
}
```

### Executing Skills

```go
result, err := agentInstance.ExecuteSkill(ctx, "weather", "San Francisco")
if err != nil {
    log.Fatalf("Skill execution failed: %v", err)
}
fmt.Println(result) // "Weather for San Francisco: Sunny, 72°F"
```

## Skill Structure

### Required Fields

- **Name** (string): Unique identifier for the skill
- **Description** (string): Brief description of what the skill does
- **Handler** (SkillHandler): Function that executes the skill logic

### Optional Fields

- **Instructions** (string): Guidance for agents on when to use the skill
- **Metadata** (map[string]interface{}): Additional information about the skill

## Skill Registry

The `SkillRegistry` manages a collection of skills and provides methods for registration, retrieval, and execution.

### Creating a Registry

```go
registry := agent.NewSkillRegistry()
```

### Registry Operations

```go
// Register a skill
err := registry.Register(skill)

// Check if a skill exists
exists := registry.Has("weather")

// Get a skill
skill, found := registry.Get("weather")

// Execute a skill
result, err := registry.Execute(ctx, "weather", "New York")

// List all skills
skills := registry.List()

// Get skill names
names := registry.Names()

// Remove a skill
registry.Unregister("weather")

// Count skills
count := registry.Count()

// Clear all skills
registry.Clear()
```

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "strings"

    "github.com/digitallysavvy/go-ai/pkg/agent"
    "github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
    // Create agent
    config := agent.AgentConfig{
        Model:    openai.NewLanguageModel("gpt-4o-mini", openai.Config{APIKey: apiKey}),
        System:   "You are a helpful assistant with text processing skills.",
        MaxSteps: 5,
    }
    agentInstance := agent.NewToolLoopAgent(config)

    // Define skills
    uppercaseSkill := &agent.Skill{
        Name:        "uppercase",
        Description: "Converts text to uppercase",
        Instructions: "Use when the user wants text in all caps",
        Handler: func(ctx context.Context, input string) (string, error) {
            return strings.ToUpper(input), nil
        },
    }

    wordCountSkill := &agent.Skill{
        Name:        "word_count",
        Description: "Counts words in text",
        Instructions: "Use when the user wants to know word count",
        Handler: func(ctx context.Context, input string) (string, error) {
            words := strings.Fields(input)
            return fmt.Sprintf("Word count: %d", len(words)), nil
        },
        Metadata: map[string]interface{}{
            "category": "text-analysis",
            "version":  "1.0",
        },
    }

    // Add skills
    agentInstance.AddSkill(uppercaseSkill)
    agentInstance.AddSkill(wordCountSkill)

    // Execute skills
    result1, _ := agentInstance.ExecuteSkill(context.Background(), "uppercase", "hello world")
    fmt.Println(result1) // "HELLO WORLD"

    result2, _ := agentInstance.ExecuteSkill(context.Background(), "word_count", "hello world")
    fmt.Println(result2) // "Word count: 2"

    // List skills
    for _, skill := range agentInstance.ListSkills() {
        fmt.Printf("Skill: %s - %s\n", skill.Name, skill.Description)
    }
}
```

## Best Practices

### 1. Choose Clear Names

Use descriptive, action-oriented names for skills:

```go
// Good
"format_text"
"analyze_sentiment"
"extract_keywords"

// Avoid
"skill1"
"helper"
"util"
```

### 2. Provide Detailed Instructions

Help agents understand when to use each skill:

```go
skill := &agent.Skill{
    Name:        "summarize",
    Description: "Creates a concise summary of text",
    Instructions: "Use this skill when the user asks for a summary, synopsis, or brief overview of long text. Works best with text over 100 words.",
    Handler: summarizeHandler,
}
```

### 3. Handle Errors Gracefully

Return descriptive errors from skill handlers:

```go
Handler: func(ctx context.Context, input string) (string, error) {
    if input == "" {
        return "", fmt.Errorf("input cannot be empty")
    }

    result, err := processData(input)
    if err != nil {
        return "", fmt.Errorf("processing failed: %w", err)
    }

    return result, nil
}
```

### 4. Use Metadata for Organization

Add metadata to categorize and version skills:

```go
skill := &agent.Skill{
    Name:        "analyze_sentiment",
    Description: "Analyzes text sentiment",
    Handler:     sentimentHandler,
    Metadata: map[string]interface{}{
        "category": "text-analysis",
        "version":  "1.0.0",
        "author":   "team-ai",
        "model":    "sentiment-v2",
    },
}
```

### 5. Keep Skills Focused

Each skill should do one thing well:

```go
// Good: Focused skills
formatSkill      // Just formats text
validateSkill    // Just validates
transformSkill   // Just transforms

// Avoid: Overly complex skills
formatAndValidateAndTransformSkill
```

## Skills vs Tools

### When to Use Skills

- **Reusable agent behaviors**: When multiple agents need the same capability
- **Simple operations**: When the operation doesn't need LLM-style tool calling
- **Organizational clarity**: When you want to group related agent behaviors

### When to Use Tools

- **LLM-driven operations**: When the LLM should decide when to use it
- **Complex parameters**: When you need structured input schemas
- **Tool calling**: When using the model's native tool-calling capabilities

### Skills + Tools

You can use both together:

```go
// Tool for LLM to call
weatherTool := types.Tool{
    Name:        "get_weather",
    Description: "Get current weather",
    Execute: func(ctx context.Context, args map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
        location := args["location"].(string)
        // Fetch weather...
        return weatherData, nil
    },
}

// Skill for direct agent use
weatherSkill := &agent.Skill{
    Name:        "format_weather",
    Description: "Formats weather data for display",
    Handler: func(ctx context.Context, input string) (string, error) {
        // Format weather...
        return formatted, nil
    },
}

config := agent.AgentConfig{
    Tools:  []types.Tool{weatherTool},
    Skills: registry,
}
```

## Advanced Patterns

### Skill Composition

Combine multiple skills:

```go
compositeSkill := &agent.Skill{
    Name: "process_and_analyze",
    Handler: func(ctx context.Context, input string) (string, error) {
        // Use other skills
        processed, err := agentInstance.ExecuteSkill(ctx, "process", input)
        if err != nil {
            return "", err
        }

        analyzed, err := agentInstance.ExecuteSkill(ctx, "analyze", processed)
        return analyzed, err
    },
}
```

### Conditional Skills

Add/remove skills dynamically:

```go
if userHasPremium {
    agentInstance.AddSkill(premiumSkill)
}

if !userNeedsBasicFeatures {
    agentInstance.RemoveSkill("basic_feature")
}
```

### Skill Middleware

Wrap skill execution with common logic:

```go
func withLogging(skill *agent.Skill) *agent.Skill {
    originalHandler := skill.Handler

    skill.Handler = func(ctx context.Context, input string) (string, error) {
        log.Printf("Executing skill: %s with input: %s", skill.Name, input)

        result, err := originalHandler(ctx, input)

        if err != nil {
            log.Printf("Skill %s failed: %v", skill.Name, err)
        } else {
            log.Printf("Skill %s succeeded", skill.Name)
        }

        return result, err
    }

    return skill
}

// Usage
agentInstance.AddSkill(withLogging(mySkill))
```

## See Also

- [Agent Subagents](./agent-subagents.md) - Hierarchical agent delegation
- [Tool Loop Agent](./tool-loop-agent.md) - Core agent implementation
- [Agent Configuration](./agent-configuration.md) - Configuring agents

## Examples

See the [agent-skills example](../../examples/agent-skills/) for a complete working example.

# Agent Subagents

Subagents enable hierarchical agent systems where a main agent can delegate tasks to specialized subagents. This allows for complex workflows with division of labor and specialized expertise.

## Overview

Subagents in the Go-AI SDK allow you to:
- **Delegate specialized tasks**: Route work to agents optimized for specific domains
- **Build hierarchies**: Create multi-level agent structures with subagents having their own subagents
- **Separate concerns**: Keep agents focused on their areas of expertise
- **Coordinate workflows**: Orchestrate complex tasks across multiple specialized agents

## Basic Usage

### Creating Subagents

```go
// Create main agent
mainConfig := agent.AgentConfig{
    Model:    model,
    System:   "You are a coordinator agent",
    MaxSteps: 5,
}
mainAgent := agent.NewToolLoopAgent(mainConfig)

// Create research subagent
researchConfig := agent.AgentConfig{
    Model:    model,
    System:   "You are a research specialist",
    MaxSteps: 3,
}
researchAgent := agent.NewToolLoopAgent(researchConfig)

// Register subagent
if err := mainAgent.AddSubagent("research", researchAgent); err != nil {
    log.Fatalf("Failed to add subagent: %v", err)
}
```

### Delegating to Subagents

```go
// Delegate with a prompt
result, err := mainAgent.DelegateToSubagent(
    ctx,
    "research",
    "Find information about Go concurrency patterns",
)

// Delegate with messages
messages := []types.Message{
    {
        Role: types.RoleUser,
        Content: []types.ContentPart{
            types.TextContent{Text: "Analyze this data"},
        },
    },
}

result, err := mainAgent.DelegateToSubagentWithMessages(
    ctx,
    "research",
    messages,
)
```

## Subagent Registry

The `SubagentRegistry` manages a collection of subagents and provides methods for registration, retrieval, and delegation.

### Creating a Registry

```go
registry := agent.NewSubagentRegistry()
```

### Registry Operations

```go
// Register a subagent
err := registry.Register("research", researchAgent)

// Check if a subagent exists
exists := registry.Has("research")

// Get a subagent
subagent, found := registry.Get("research")

// Execute delegation
result, err := registry.Execute(ctx, "research", "find data")

// Execute with messages
result, err := registry.ExecuteWithMessages(ctx, "research", messages)

// List all subagents
names := registry.List()

// Remove a subagent
registry.Unregister("research")

// Count subagents
count := registry.Count()

// Clear all subagents
registry.Clear()
```

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/digitallysavvy/go-ai/pkg/agent"
    "github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
    model := openai.NewLanguageModel("gpt-4o-mini", openai.Config{APIKey: apiKey})

    // Create main coordinator agent
    mainConfig := agent.AgentConfig{
        Model:    model,
        System:   "You coordinate tasks between specialized agents",
        MaxSteps: 5,
    }
    mainAgent := agent.NewToolLoopAgent(mainConfig)

    // Create research subagent
    researchConfig := agent.AgentConfig{
        Model:    model,
        System:   "You are a research specialist who finds and summarizes information",
        MaxSteps: 3,
    }
    researchAgent := agent.NewToolLoopAgent(researchConfig)

    // Create analysis subagent
    analysisConfig := agent.AgentConfig{
        Model:    model,
        System:   "You are an analysis specialist who provides insights from data",
        MaxSteps: 3,
    }
    analysisAgent := agent.NewToolLoopAgent(analysisConfig)

    // Register subagents
    mainAgent.AddSubagent("research", researchAgent)
    mainAgent.AddSubagent("analysis", analysisAgent)

    // Delegate to research subagent
    researchResult, err := mainAgent.DelegateToSubagent(
        context.Background(),
        "research",
        "Find information about Go interfaces",
    )
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("Research result:", researchResult.Text)

    // Delegate to analysis subagent
    analysisResult, err := mainAgent.DelegateToSubagent(
        context.Background(),
        "analysis",
        "Analyze the benefits of using interfaces",
    )
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("Analysis result:", analysisResult.Text)
}
```

## Hierarchical Agents

Subagents can have their own subagents, creating multi-level hierarchies:

```go
// Main agent
mainAgent := agent.NewToolLoopAgent(mainConfig)

// Research agent (subagent of main)
researchAgent := agent.NewToolLoopAgent(researchConfig)
mainAgent.AddSubagent("research", researchAgent)

// Deep research agent (subagent of research)
deepResearchAgent := agent.NewToolLoopAgent(deepResearchConfig)

// Cast to ToolLoopAgent to access subagent methods
researchToolLoopAgent := researchAgent.(*agent.ToolLoopAgent)
researchToolLoopAgent.AddSubagent("deep_research", deepResearchAgent)

// Delegate through hierarchy
// Main -> Research
result1, _ := mainAgent.DelegateToSubagent(ctx, "research", "broad research task")

// Research -> Deep Research
result2, _ := researchToolLoopAgent.DelegateToSubagent(ctx, "deep_research", "detailed research task")
```

## Delegation Tracking

Track delegation history with `DelegationTracker`:

```go
tracker := agent.NewDelegationTracker()

// Track a delegation
delegation := agent.SubagentDelegation{
    SubagentName: "research",
    Prompt:       "Find data",
    Result:       result,
}
tracker.Track(delegation)

// Get all delegations
delegations := tracker.GetDelegations()

for _, d := range delegations {
    fmt.Printf("Delegated to %s: %s\n", d.SubagentName, d.Prompt)
    if d.Result != nil {
        fmt.Printf("Result: %s\n", d.Result.Text)
    }
}
```

## Best Practices

### 1. Clear Specialization

Give each subagent a clear, focused role:

```go
// Good: Specialized subagents
researchAgent   // Finds information
analysisAgent   // Analyzes data
summaryAgent    // Creates summaries

// Avoid: Generic subagents
helperAgent     // Too vague
utilityAgent    // Unclear purpose
```

### 2. Appropriate System Prompts

Tailor system prompts to each subagent's specialty:

```go
researchConfig := agent.AgentConfig{
    System: `You are a research specialist. Your role is to:
    - Find relevant information on requested topics
    - Verify sources and accuracy
    - Provide comprehensive summaries
    - Focus on factual, well-sourced content`,
}

analysisConfig := agent.AgentConfig{
    System: `You are a data analysis specialist. Your role is to:
    - Analyze patterns and trends in data
    - Provide insights and interpretations
    - Identify key findings
    - Present clear, actionable conclusions`,
}
```

### 3. Manage Subagent Scope

Limit subagent steps to prevent runaway execution:

```go
subagentConfig := agent.AgentConfig{
    Model:    model,
    MaxSteps: 3,  // Limit subagent steps
}

mainConfig := agent.AgentConfig{
    Model:    model,
    MaxSteps: 10, // Main agent can take more steps
}
```

### 4. Error Handling

Handle delegation failures gracefully:

```go
result, err := mainAgent.DelegateToSubagent(ctx, "research", prompt)
if err != nil {
    // Log the error
    log.Printf("Delegation to research failed: %v", err)

    // Try alternative approach
    result, err = mainAgent.DelegateToSubagent(ctx, "backup_research", prompt)

    // Or handle in main agent
    if err != nil {
        return mainAgent.Execute(ctx, "Handle this yourself: "+prompt)
    }
}
```

### 5. Combine with Skills

Subagents can have their own skills:

```go
// Create subagent with skills
analysisAgent := agent.NewToolLoopAgent(analysisConfig)

// Add skills to subagent
statisticsSkill := &agent.Skill{
    Name:        "calculate_stats",
    Description: "Calculates statistics",
    Handler:     statsHandler,
}
analysisAgent.AddSkill(statisticsSkill)

// Register as subagent
mainAgent.AddSubagent("analysis", analysisAgent)

// Access subagent's skills
subagent, _ := mainAgent.GetSubagent("analysis")
toolLoopSubagent := subagent.(*agent.ToolLoopAgent)
result, _ := toolLoopSubagent.ExecuteSkill(ctx, "calculate_stats", data)
```

## Design Patterns

### 1. Coordinator Pattern

Main agent coordinates work across subagents:

```go
mainAgent := agent.NewToolLoopAgent(coordinatorConfig)
mainAgent.AddSubagent("research", researchAgent)
mainAgent.AddSubagent("analysis", analysisAgent)
mainAgent.AddSubagent("writer", writerAgent)

// Main agent decides which subagent to use
result, _ := mainAgent.Execute(ctx, "Create a report on AI trends")
```

### 2. Pipeline Pattern

Subagents form a processing pipeline:

```go
// Step 1: Research
researchResult, _ := mainAgent.DelegateToSubagent(ctx, "research", topic)

// Step 2: Analyze research
analysisResult, _ := mainAgent.DelegateToSubagent(ctx, "analysis", researchResult.Text)

// Step 3: Summarize analysis
summaryResult, _ := mainAgent.DelegateToSubagent(ctx, "writer", analysisResult.Text)
```

### 3. Expert Panel Pattern

Multiple subagents provide different perspectives:

```go
// Get opinions from multiple experts
technicalView, _ := mainAgent.DelegateToSubagent(ctx, "technical_expert", question)
businessView, _ := mainAgent.DelegateToSubagent(ctx, "business_expert", question)
legalView, _ := mainAgent.DelegateToSubagent(ctx, "legal_expert", question)

// Main agent synthesizes
synthesis := fmt.Sprintf(
    "Technical: %s\nBusiness: %s\nLegal: %s",
    technicalView.Text,
    businessView.Text,
    legalView.Text,
)
```

### 4. Hierarchical Delegation

Multi-level delegation for complex tasks:

```go
// Level 1: Main agent
mainAgent.AddSubagent("operations", operationsAgent)

// Level 2: Operations has sub-specialists
operationsAgent.AddSubagent("data_processing", dataAgent)
operationsAgent.AddSubagent("quality_control", qcAgent)

// Level 3: Data processing has its own subagents
dataAgent.AddSubagent("cleaning", cleaningAgent)
dataAgent.AddSubagent("transformation", transformAgent)
```

## Subagents vs Tools

### When to Use Subagents

- **Complex specialized tasks**: When a task requires its own agent loop
- **Domain expertise**: When you need focused, specialized behavior
- **Workflow steps**: When breaking a task into distinct phases
- **Resource management**: When you want separate token/cost tracking

### When to Use Tools

- **Simple operations**: When a single function call suffices
- **LLM-driven decisions**: When the model should choose when to call
- **Structured I/O**: When you need parameter schemas
- **Native integration**: When using model's tool-calling features

### Subagents + Tools

Use both for maximum flexibility:

```go
// Subagent with tools
researchAgent := agent.NewToolLoopAgent(researchConfig)

// Add tools to subagent
researchAgent.config.Tools = []types.Tool{
    searchTool,
    summarizerTool,
}

// Register as subagent
mainAgent.AddSubagent("research", researchAgent)

// Subagent can use its tools when delegated to
result, _ := mainAgent.DelegateToSubagent(ctx, "research", "find information")
```

## Performance Considerations

### Token Usage

Subagents add overhead:

```go
// Each delegation is a full agent execution
result, _ := mainAgent.DelegateToSubagent(ctx, "research", prompt)
// Uses tokens for: main agent + research agent + context passing
```

### Limiting Costs

Control subagent resource usage:

```go
subagentConfig := agent.AgentConfig{
    Model:    model,
    MaxSteps: 2,           // Limit steps
    MaxTokens: &maxTokens,  // Limit tokens per step
}
```

### Caching

Reuse subagents instead of recreating:

```go
// Good: Create once, reuse
researchAgent := agent.NewToolLoopAgent(config)
mainAgent.AddSubagent("research", researchAgent)

// Multiple delegations reuse the same subagent
result1, _ := mainAgent.DelegateToSubagent(ctx, "research", prompt1)
result2, _ := mainAgent.DelegateToSubagent(ctx, "research", prompt2)
```

## See Also

- [Agent Skills](./agent-skills.md) - Reusable agent behaviors
- [Tool Loop Agent](./tool-loop-agent.md) - Core agent implementation
- [Agent Configuration](./agent-configuration.md) - Configuring agents

## Examples

See these examples for complete working code:
- [agent-subagents example](../../examples/agent-subagents/) - Basic subagent usage
- [agent-skills-subagents example](../../examples/agent-skills-subagents/) - Skills and subagents together

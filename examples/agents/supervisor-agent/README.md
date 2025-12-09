# Supervisor Agent

Demonstrates a supervisor agent that manages and coordinates multiple specialized sub-agents to complete complex tasks.

## Overview

This example shows how to build a hierarchical agent system where a supervisor agent:
- Receives complex tasks
- Breaks them down into subtasks
- Delegates to specialized sub-agents
- Synthesizes results into final output

## Architecture

```
┌─────────────────────────────────┐
│     Supervisor Agent            │
│  - Task planning                │
│  - Agent coordination           │
│  - Result synthesis             │
└────────┬────────────────────────┘
         │
         ├─────────┬────────┬────────┐
         │         │        │        │
    ┌────▼───┐ ┌──▼───┐ ┌──▼───┐ ┌─▼────┐
    │Research│ │Writer│ │Review│ │Editor│
    │ Agent  │ │Agent │ │Agent │ │Agent │
    └────────┘ └──────┘ └──────┘ └──────┘
```

## Features

- **Hierarchical coordination**: Supervisor manages multiple worker agents
- **Dynamic task planning**: Supervisor creates workflow plans
- **Specialized agents**: Each sub-agent has a specific role
- **Result synthesis**: Supervisor combines outputs
- **Delegation tool**: Tool-based agent delegation

## Prerequisites

```bash
export OPENAI_API_KEY=sk-...
```

## Usage

```bash
go run main.go
```

## How It Works

### 1. Sub-Agent Definition

```go
type SubAgent struct {
    name        string
    role        string
    model       provider.LanguageModel
    systemPrompt string
}
```

Each sub-agent is specialized for a specific task (research, writing, review, editing).

### 2. Supervisor Setup

```go
supervisor := &SupervisorAgent{
    model:     model,
    subAgents: make(map[string]*SubAgent),
}

supervisor.RegisterSubAgent("researcher", &SubAgent{
    name:  "Researcher",
    role:  "research",
    model: model,
    systemPrompt: "You are a research specialist...",
})
```

### 3. Task Execution Flow

1. **Task Reception**: Supervisor receives a complex task
2. **Planning**: Supervisor creates a step-by-step plan
3. **Delegation**: Supervisor assigns steps to appropriate sub-agents
4. **Execution**: Each sub-agent completes its assigned task
5. **Synthesis**: Supervisor combines results into final output

### 4. Workflow Planning

```go
plan := s.planWorkflow(ctx, task)
// Returns format: "1. [researcher] - gather info about Go"
```

### 5. Sub-Agent Execution

```go
result := agent.Execute(ctx, instruction)
```

Each agent uses its specialized system prompt to complete the task.

## Example Output

```
=== Example 1: Supervised Multi-Agent Workflow ===

🎯 Supervisor received task: Create a blog post about Go programming

📋 Supervisor's Plan:
1. [researcher] - Research benefits and features of Go
2. [writer] - Write blog post based on research
3. [reviewer] - Review the blog post for quality
4. [editor] - Edit and finalize based on feedback

Step 1: [researcher] - Research benefits and features of Go
  🤖 Researcher working on: Research benefits and features of Go
  📝 Result: Go offers fast compilation, built-in concurrency...
✅ Researcher completed

Step 2: [writer] - Write blog post based on research
  🤖 Writer working on: Write blog post based on research
  📝 Result: # Why Choose Go for Your Next Project...
✅ Writer completed

Step 3: [reviewer] - Review the blog post for quality
  🤖 Reviewer working on: Review the blog post for quality
  📝 Result: The post is well-structured. Consider adding...
✅ Reviewer completed

Step 4: [editor] - Edit and finalize based on feedback
  🤖 Editor working on: Edit and finalize based on feedback
  📝 Result: [Polished final version]
✅ Editor completed

Final Result:
[Complete, polished blog post about Go]
```

## Use Cases

### 1. Content Creation Pipeline
- Research → Write → Review → Edit
- Automated content workflow
- Quality assurance built-in

### 2. Code Review System
- Code analysis → Security review → Performance check → Final approval
- Multiple specialized reviewers
- Comprehensive evaluation

### 3. Customer Support
- Issue classification → Investigation → Solution proposal → Response drafting
- Specialized expertise for each step
- Consistent quality

### 4. Data Analysis Workflow
- Data collection → Analysis → Validation → Report generation
- Domain experts for each phase
- Thorough analysis

## Supervisor Patterns

### Pattern 1: Sequential Workflow
```
Task → Agent1 → Agent2 → Agent3 → Result
```

### Pattern 2: Parallel Execution
```
        ┌─→ Agent1 ─┐
Task ──┼─→ Agent2 ──┼→ Synthesize → Result
        └─→ Agent3 ─┘
```

### Pattern 3: Conditional Routing
```
Task → Supervisor Decision
         ├─→ if research needed → Researcher
         ├─→ if writing needed → Writer
         └─→ if review needed → Reviewer
```

## Benefits

1. **Separation of Concerns**: Each agent specializes in one task
2. **Reusability**: Agents can be reused across workflows
3. **Scalability**: Easy to add new specialized agents
4. **Quality**: Multiple passes ensure high-quality output
5. **Maintainability**: Clear responsibilities and boundaries

## Advanced Features

### Dynamic Agent Selection

The supervisor can choose which agents to use based on task requirements:

```go
// Supervisor analyzes task and selects appropriate agents
workflow := supervisor.analyzeTa sk(task)
```

### Tool-Based Delegation

```go
delegationTool := types.Tool{
    Name: "delegate_to_agent",
    Description: "Delegate task to a sub-agent",
    Execute: func(ctx context.Context, params map[string]interface{}) {
        // Delegate to appropriate agent
    },
}
```

## Best Practices

1. **Clear System Prompts**: Give each agent a well-defined role
2. **Task Boundaries**: Ensure tasks are appropriately scoped
3. **Error Handling**: Handle failures gracefully
4. **Result Validation**: Verify sub-agent outputs
5. **Iterative Refinement**: Allow feedback loops between agents

## Extending the Example

### Add More Agents

```go
supervisor.RegisterSubAgent("translator", &SubAgent{
    name:  "Translator",
    role:  "translation",
    model: model,
    systemPrompt: "You are a professional translator...",
})
```

### Add Memory/State

```go
type SupervisorAgent struct {
    model     provider.LanguageModel
    subAgents map[string]*SubAgent
    memory    []Message  // Track conversation history
    state     TaskState  // Track workflow state
}
```

### Add Agent Communication

```go
// Allow agents to request help from other agents
func (a *SubAgent) RequestHelp(ctx context.Context, fromAgent string, question string) string {
    // Agent-to-agent communication
}
```

## Comparison with Multi-Agent

| Feature | Multi-Agent | Supervisor-Agent |
|---------|-------------|------------------|
| Coordination | Flat/peer-to-peer | Hierarchical |
| Planning | Distributed | Centralized |
| Complexity | Simple workflows | Complex workflows |
| Control | Shared | Supervisor-led |

## Related Examples

- [multi-agent](../multi-agent) - Peer-to-peer agent coordination
- [math-agent](../math-agent) - Multi-tool single agent
- [web-search-agent](../web-search-agent) - Specialized research agent
- [streaming-agent](../streaming-agent) - Real-time agent updates

## Further Reading

- [Agent Architecture Patterns](https://docs.anthropic.com/agents)
- [LangGraph Multi-Agent Systems](https://python.langchain.com/docs/langgraph)
- [AutoGPT Architecture](https://github.com/Significant-Gravitas/AutoGPT)

## License

MIT

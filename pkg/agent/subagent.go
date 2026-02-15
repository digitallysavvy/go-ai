package agent

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// SubagentRegistry manages a collection of subagents
// It allows an agent to delegate tasks to specialized subagents
type SubagentRegistry struct {
	subagents map[string]Agent
}

// NewSubagentRegistry creates a new subagent registry
func NewSubagentRegistry() *SubagentRegistry {
	return &SubagentRegistry{
		subagents: make(map[string]Agent),
	}
}

// Register adds a subagent to the registry
// Returns an error if a subagent with the same name already exists
func (r *SubagentRegistry) Register(name string, agent Agent) error {
	if name == "" {
		return fmt.Errorf("subagent name cannot be empty")
	}

	if agent == nil {
		return fmt.Errorf("subagent cannot be nil")
	}

	if _, exists := r.subagents[name]; exists {
		return fmt.Errorf("subagent '%s' already registered", name)
	}

	r.subagents[name] = agent
	return nil
}

// Unregister removes a subagent from the registry
func (r *SubagentRegistry) Unregister(name string) {
	delete(r.subagents, name)
}

// Get retrieves a subagent by name
// Returns the subagent and true if found, nil and false otherwise
func (r *SubagentRegistry) Get(name string) (Agent, bool) {
	agent, exists := r.subagents[name]
	return agent, exists
}

// Has checks if a subagent exists in the registry
func (r *SubagentRegistry) Has(name string) bool {
	_, exists := r.subagents[name]
	return exists
}

// List returns all registered subagent names
func (r *SubagentRegistry) List() []string {
	names := make([]string, 0, len(r.subagents))
	for name := range r.subagents {
		names = append(names, name)
	}
	return names
}

// Execute delegates execution to a named subagent
// Returns an error if the subagent is not found or execution fails
func (r *SubagentRegistry) Execute(ctx context.Context, name string, prompt string) (*AgentResult, error) {
	agent, exists := r.subagents[name]
	if !exists {
		return nil, fmt.Errorf("subagent '%s' not found", name)
	}

	return agent.Execute(ctx, prompt)
}

// ExecuteWithMessages delegates execution to a named subagent with message history
// Returns an error if the subagent is not found or execution fails
func (r *SubagentRegistry) ExecuteWithMessages(ctx context.Context, name string, messages []types.Message) (*AgentResult, error) {
	agent, exists := r.subagents[name]
	if !exists {
		return nil, fmt.Errorf("subagent '%s' not found", name)
	}

	return agent.ExecuteWithMessages(ctx, messages)
}

// Clear removes all subagents from the registry
func (r *SubagentRegistry) Clear() {
	r.subagents = make(map[string]Agent)
}

// Count returns the number of registered subagents
func (r *SubagentRegistry) Count() int {
	return len(r.subagents)
}

// SubagentDelegation represents a delegation request to a subagent
// This can be used to track delegation history and results
type SubagentDelegation struct {
	// Name of the subagent
	SubagentName string

	// Input prompt or messages
	Prompt   string
	Messages []types.Message

	// Result of the delegation
	Result *AgentResult

	// Error if delegation failed
	Error error
}

// DelegationTracker tracks subagent delegations during agent execution
type DelegationTracker struct {
	delegations []SubagentDelegation
}

// NewDelegationTracker creates a new delegation tracker
func NewDelegationTracker() *DelegationTracker {
	return &DelegationTracker{
		delegations: make([]SubagentDelegation, 0),
	}
}

// Track adds a delegation to the tracker
func (t *DelegationTracker) Track(delegation SubagentDelegation) {
	t.delegations = append(t.delegations, delegation)
}

// GetDelegations returns all tracked delegations
func (t *DelegationTracker) GetDelegations() []SubagentDelegation {
	return t.delegations
}

// Count returns the number of tracked delegations
func (t *DelegationTracker) Count() int {
	return len(t.delegations)
}

// Clear removes all tracked delegations
func (t *DelegationTracker) Clear() {
	t.delegations = make([]SubagentDelegation, 0)
}

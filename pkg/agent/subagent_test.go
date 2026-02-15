package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// mockAgent is a simple mock implementation of the Agent interface
type mockAgent struct {
	executeFunc            func(ctx context.Context, prompt string) (*AgentResult, error)
	executeWithMessagesFunc func(ctx context.Context, messages []types.Message) (*AgentResult, error)
}

func (m *mockAgent) Execute(ctx context.Context, prompt string) (*AgentResult, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, prompt)
	}
	return &AgentResult{
		Text: "mock result for: " + prompt,
	}, nil
}

func (m *mockAgent) ExecuteWithMessages(ctx context.Context, messages []types.Message) (*AgentResult, error) {
	if m.executeWithMessagesFunc != nil {
		return m.executeWithMessagesFunc(ctx, messages)
	}
	return &AgentResult{
		Text: "mock result with messages",
	}, nil
}

func TestSubagentRegistry_Register(t *testing.T) {
	registry := NewSubagentRegistry()
	agent := &mockAgent{}

	// Test successful registration
	err := registry.Register("research", agent)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Test duplicate registration
	err = registry.Register("research", agent)
	if err == nil {
		t.Fatal("expected error for duplicate registration, got nil")
	}

	// Test empty name
	err = registry.Register("", agent)
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}

	// Test nil agent
	err = registry.Register("nil-agent", nil)
	if err == nil {
		t.Fatal("expected error for nil agent, got nil")
	}
}

func TestSubagentRegistry_Get(t *testing.T) {
	registry := NewSubagentRegistry()
	agent := &mockAgent{}

	registry.Register("research", agent)

	// Test getting existing subagent
	retrieved, exists := registry.Get("research")
	if !exists {
		t.Fatal("expected subagent to exist")
	}
	if retrieved == nil {
		t.Fatal("expected non-nil subagent")
	}

	// Test getting non-existent subagent
	_, exists = registry.Get("non-existent")
	if exists {
		t.Fatal("expected subagent not to exist")
	}
}

func TestSubagentRegistry_Has(t *testing.T) {
	registry := NewSubagentRegistry()
	agent := &mockAgent{}

	registry.Register("research", agent)

	if !registry.Has("research") {
		t.Fatal("expected subagent to exist")
	}

	if registry.Has("non-existent") {
		t.Fatal("expected subagent not to exist")
	}
}

func TestSubagentRegistry_Unregister(t *testing.T) {
	registry := NewSubagentRegistry()
	agent := &mockAgent{}

	registry.Register("research", agent)

	if !registry.Has("research") {
		t.Fatal("expected subagent to exist")
	}

	registry.Unregister("research")

	if registry.Has("research") {
		t.Fatal("expected subagent to be removed")
	}
}

func TestSubagentRegistry_List(t *testing.T) {
	registry := NewSubagentRegistry()
	agent1 := &mockAgent{}
	agent2 := &mockAgent{}

	registry.Register("research", agent1)
	registry.Register("analysis", agent2)

	names := registry.List()
	if len(names) != 2 {
		t.Fatalf("expected 2 subagents, got: %d", len(names))
	}

	// Check that both names are present
	nameSet := make(map[string]bool)
	for _, name := range names {
		nameSet[name] = true
	}

	if !nameSet["research"] || !nameSet["analysis"] {
		t.Fatal("expected both research and analysis in list")
	}
}

func TestSubagentRegistry_Execute(t *testing.T) {
	registry := NewSubagentRegistry()
	agent := &mockAgent{
		executeFunc: func(ctx context.Context, prompt string) (*AgentResult, error) {
			return &AgentResult{
				Text: "Research result for: " + prompt,
			}, nil
		},
	}

	registry.Register("research", agent)

	// Test successful execution
	result, err := registry.Execute(context.Background(), "research", "find information")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result.Text != "Research result for: find information" {
		t.Fatalf("unexpected result: %s", result.Text)
	}

	// Test execution of non-existent subagent
	_, err = registry.Execute(context.Background(), "non-existent", "test")
	if err == nil {
		t.Fatal("expected error for non-existent subagent, got nil")
	}
}

func TestSubagentRegistry_ExecuteWithMessages(t *testing.T) {
	registry := NewSubagentRegistry()
	agent := &mockAgent{
		executeWithMessagesFunc: func(ctx context.Context, messages []types.Message) (*AgentResult, error) {
			return &AgentResult{
				Text: fmt.Sprintf("Processed %d messages", len(messages)),
			}, nil
		},
	}

	registry.Register("research", agent)

	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "test message"},
			},
		},
	}

	// Test successful execution with messages
	result, err := registry.ExecuteWithMessages(context.Background(), "research", messages)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result.Text != "Processed 1 messages" {
		t.Fatalf("unexpected result: %s", result.Text)
	}

	// Test execution of non-existent subagent
	_, err = registry.ExecuteWithMessages(context.Background(), "non-existent", messages)
	if err == nil {
		t.Fatal("expected error for non-existent subagent, got nil")
	}
}

func TestSubagentRegistry_Clear(t *testing.T) {
	registry := NewSubagentRegistry()
	agent1 := &mockAgent{}
	agent2 := &mockAgent{}

	registry.Register("research", agent1)
	registry.Register("analysis", agent2)

	if registry.Count() != 2 {
		t.Fatalf("expected 2 subagents, got: %d", registry.Count())
	}

	registry.Clear()

	if registry.Count() != 0 {
		t.Fatalf("expected 0 subagents after clear, got: %d", registry.Count())
	}
}

func TestSubagentRegistry_Count(t *testing.T) {
	registry := NewSubagentRegistry()

	if registry.Count() != 0 {
		t.Fatalf("expected count 0, got: %d", registry.Count())
	}

	agent := &mockAgent{}
	registry.Register("research", agent)

	if registry.Count() != 1 {
		t.Fatalf("expected count 1, got: %d", registry.Count())
	}
}

func TestDelegationTracker_Track(t *testing.T) {
	tracker := NewDelegationTracker()

	delegation := SubagentDelegation{
		SubagentName: "research",
		Prompt:       "find information",
		Result: &AgentResult{
			Text: "result",
		},
	}

	tracker.Track(delegation)

	if tracker.Count() != 1 {
		t.Fatalf("expected 1 delegation, got: %d", tracker.Count())
	}
}

func TestDelegationTracker_GetDelegations(t *testing.T) {
	tracker := NewDelegationTracker()

	delegation1 := SubagentDelegation{
		SubagentName: "research",
		Prompt:       "find information",
	}

	delegation2 := SubagentDelegation{
		SubagentName: "analysis",
		Prompt:       "analyze data",
	}

	tracker.Track(delegation1)
	tracker.Track(delegation2)

	delegations := tracker.GetDelegations()
	if len(delegations) != 2 {
		t.Fatalf("expected 2 delegations, got: %d", len(delegations))
	}

	if delegations[0].SubagentName != "research" {
		t.Fatalf("expected first delegation to be 'research', got: %s", delegations[0].SubagentName)
	}

	if delegations[1].SubagentName != "analysis" {
		t.Fatalf("expected second delegation to be 'analysis', got: %s", delegations[1].SubagentName)
	}
}

func TestDelegationTracker_Clear(t *testing.T) {
	tracker := NewDelegationTracker()

	delegation := SubagentDelegation{
		SubagentName: "research",
		Prompt:       "find information",
	}

	tracker.Track(delegation)

	if tracker.Count() != 1 {
		t.Fatalf("expected 1 delegation, got: %d", tracker.Count())
	}

	tracker.Clear()

	if tracker.Count() != 0 {
		t.Fatalf("expected 0 delegations after clear, got: %d", tracker.Count())
	}
}

func TestSubagentDelegation_WithError(t *testing.T) {
	delegation := SubagentDelegation{
		SubagentName: "research",
		Prompt:       "find information",
		Error:        fmt.Errorf("delegation failed"),
	}

	if delegation.Error == nil {
		t.Fatal("expected error to be set")
	}

	if delegation.Error.Error() != "delegation failed" {
		t.Fatalf("expected error message 'delegation failed', got: %s", delegation.Error.Error())
	}
}

func TestSubagentDelegation_WithMessages(t *testing.T) {
	messages := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "test message"},
			},
		},
	}

	delegation := SubagentDelegation{
		SubagentName: "research",
		Messages:     messages,
		Result: &AgentResult{
			Text: "result",
		},
	}

	if len(delegation.Messages) != 1 {
		t.Fatalf("expected 1 message, got: %d", len(delegation.Messages))
	}

	if delegation.Result == nil {
		t.Fatal("expected result to be set")
	}
}

func TestSubagentRegistry_ExecuteWithError(t *testing.T) {
	registry := NewSubagentRegistry()
	agent := &mockAgent{
		executeFunc: func(ctx context.Context, prompt string) (*AgentResult, error) {
			return nil, fmt.Errorf("execution error")
		},
	}

	registry.Register("error-agent", agent)

	_, err := registry.Execute(context.Background(), "error-agent", "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if err.Error() != "execution error" {
		t.Fatalf("expected 'execution error', got: %v", err)
	}
}

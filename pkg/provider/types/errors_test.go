package types

import (
	"errors"
	"testing"
)

func TestMissingToolResultErrorFormatting(t *testing.T) {
	errWithMessage := (&MissingToolResultError{
		ToolCallID: "call-1",
		ToolName:   "search",
		Message:    "provider returned no output",
	}).Error()
	if errWithMessage != "missing tool result for call call-1 (tool: search): provider returned no output" {
		t.Fatalf("unexpected error string: %q", errWithMessage)
	}

	errNoMessage := (&MissingToolResultError{
		ToolCallID: "call-2",
		ToolName:   "lookup",
	}).Error()
	if errNoMessage != "missing tool result for call call-2 (tool: lookup)" {
		t.Fatalf("unexpected error string: %q", errNoMessage)
	}
}

func TestMissingToolResultsErrorFormatting(t *testing.T) {
	errWithMessage := (&MissingToolResultsError{
		ToolCallIDs: []string{"a", "b"},
		Message:     "deferred provider results missing",
	}).Error()
	if errWithMessage != "missing tool results for 2 call(s) [a b]: deferred provider results missing" {
		t.Fatalf("unexpected error string: %q", errWithMessage)
	}

	errNoMessage := (&MissingToolResultsError{
		ToolCallIDs: []string{"a"},
	}).Error()
	if errNoMessage != "missing tool results for 1 call(s): [a]" {
		t.Fatalf("unexpected error string: %q", errNoMessage)
	}
}

func TestToolExecutionErrorFormattingAndUnwrap(t *testing.T) {
	inner := errors.New("timeout")
	local := &ToolExecutionError{
		ToolCallID:       "call-1",
		ToolName:         "search",
		Err:              inner,
		ProviderExecuted: false,
	}
	if local.Error() != "tool execution failed [local] (tool: search, call: call-1): timeout" {
		t.Fatalf("unexpected local error string: %q", local.Error())
	}
	if !errors.Is(local, inner) {
		t.Fatal("expected local tool execution error to unwrap underlying error")
	}

	providerExecuted := &ToolExecutionError{
		ToolCallID:       "call-2",
		ToolName:         "search",
		Err:              inner,
		ProviderExecuted: true,
	}
	if providerExecuted.Error() != "tool execution failed [provider-executed] (tool: search, call: call-2): timeout" {
		t.Fatalf("unexpected provider-executed error string: %q", providerExecuted.Error())
	}
}

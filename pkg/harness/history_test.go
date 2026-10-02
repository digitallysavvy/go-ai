package harness

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS harness-agent.test.ts "readHistory() reads the runtime history through
// the adapter" (TS #19104 / 11f0e716d5). Ports the contract: a HarnessID
// adapter whose Session implements HistoryReader forwards ReadHistory's
// since argument to DoReadHistory and returns its result unchanged.
func TestAgentSession_ReadHistory_ReadsThroughAdapter(t *testing.T) {
	want := &ReadHistoryResult{
		Messages: []HistoryMessage{
			{Message: types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hello"}}}},
			{
				Message: types.Message{
					Role: types.RoleAssistant,
					Content: []types.ContentPart{
						types.ReasoningContent{Text: "thinking it over"},
						types.ToolCallContent{ToolCallID: "tool-1", ToolName: "bash", Input: `{"command":"ls"}`},
						types.ToolResultContent{ToolCallID: "tool-1", ToolName: "bash", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "README.md"}},
						types.TextContent{Text: "done"},
					},
				},
				At:              "2026-09-29T12:00:00.000Z",
				HarnessMetadata: Metadata{"mock": {"raw": map[string]any{"messageId": "assistant-1"}}},
			},
		},
		Cursor: "cursor-1",
	}
	var gotSince []string
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart { return nil },
		doReadHistory: func(_ context.Context, since string) (*ReadHistoryResult, error) {
			gotSince = append(gotSince, since)
			return want, nil
		},
	})
	_, session := newTestAgent(t, mock, nil, Callbacks{}, nil)

	got, err := session.ReadHistory(context.Background(), "")
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadHistory() = %+v, want %+v", got, want)
	}

	if _, err := session.ReadHistory(context.Background(), "cursor-1"); err != nil {
		t.Fatalf("ReadHistory with since: %v", err)
	}
	if !reflect.DeepEqual(gotSince, []string{"", "cursor-1"}) {
		t.Fatalf("DoReadHistory since args = %v, want [\"\" \"cursor-1\"]", gotSince)
	}
}

// TS "readHistory() throws HarnessCapabilityUnsupportedError when the
// adapter lacks it". newTestAgent's mock session never sets doReadHistory,
// so it does not implement HistoryReader.
func TestAgentSession_ReadHistory_CapabilityUnsupported(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{script: func(func(string, interface{})) []StreamPart { return nil }})
	_, session := newTestAgent(t, mock, nil, Callbacks{}, nil)

	_, err := session.ReadHistory(context.Background(), "")
	if !IsCapabilityUnsupportedError(err) {
		t.Fatalf("err = %v, want a CapabilityUnsupportedError", err)
	}
}

// TS "readHistory() rejects once the session is no longer active".
func TestAgentSession_ReadHistory_RejectsOnceInactive(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart { return nil },
		doReadHistory: func(context.Context, string) (*ReadHistoryResult, error) {
			return &ReadHistoryResult{Cursor: "c"}, nil
		},
	})
	_, session := newTestAgent(t, mock, nil, Callbacks{}, nil)

	if err := session.Destroy(context.Background()); err != nil {
		t.Fatalf("Destroy: %v", err)
	}

	_, err := session.ReadHistory(context.Background(), "")
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := err.Error(); got == "" {
		t.Fatal("expected a non-empty error message")
	}
}

// TestHistoryUnavailableError ports
// harness-history-unavailable-error.test.ts.
func TestHistoryUnavailableError(t *testing.T) {
	err := NewHistoryUnavailableError("The transcript store is inside a remote sandbox", "", nil)
	if !IsHarnessError(err) {
		t.Fatal("HistoryUnavailableError must be a HarnessError")
	}
	if !IsHistoryUnavailableError(err) {
		t.Fatal("IsHistoryUnavailableError must report true for its own error")
	}

	cause := context.DeadlineExceeded
	err2 := NewHistoryUnavailableError("No transcript directory for this working directory", "claude-code", cause)
	if err2.Message != "No transcript directory for this working directory" {
		t.Fatalf("Message = %q", err2.Message)
	}
	if err2.HarnessID != "claude-code" {
		t.Fatalf("HarnessID = %q", err2.HarnessID)
	}
	if err2.Cause != cause {
		t.Fatalf("Cause = %v, want %v", err2.Cause, cause)
	}

	if IsHistoryUnavailableError(context.DeadlineExceeded) {
		t.Fatal("IsHistoryUnavailableError must report false for unrelated errors")
	}
	if IsHistoryUnavailableError(nil) {
		t.Fatal("IsHistoryUnavailableError must report false for nil")
	}
}

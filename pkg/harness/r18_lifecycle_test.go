package harness

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type schemaCheckingHarness struct {
	*mockHarnessAdapter
	seen [][]byte
}

func (h *schemaCheckingHarness) ValidateLifecycleStateData(data json.RawMessage) error {
	h.seen = append(h.seen, append([]byte(nil), data...))
	if strings.Contains(string(data), "bad") {
		return errors.New("bad data")
	}
	return nil
}

// TS validates a resume-session's nested continueFrom recursively: harness id
// and the adapter data schema, and wraps schema failures in a HarnessError.
func TestValidateLifecycleStateDataValidatesNestedContinueFrom(t *testing.T) {
	h := &schemaCheckingHarness{mockHarnessAdapter: &mockHarnessAdapter{id: "mock"}}
	nested := func(harnessID string, data string) *ResumeSessionState {
		return &ResumeSessionState{
			Type: LifecycleStateResumeSession, HarnessID: "mock", SpecificationVersion: SpecificationVersion, Data: json.RawMessage(`{}`),
			ContinueFrom: &ContinueTurnState{
				Type: LifecycleStateContinueTurn, HarnessID: harnessID, SpecificationVersion: SpecificationVersion, Data: json.RawMessage(data),
			},
		}
	}

	if err := validateLifecycleStateData(h, nested("mock", `{}`)); err != nil {
		t.Fatalf("valid nested state rejected: %v", err)
	}
	err := validateLifecycleStateData(h, nested("other", `{}`))
	if err == nil || !strings.Contains(err.Error(), "produced by harness 'other' but this agent uses 'mock'") {
		t.Fatalf("nested harness id mismatch not rejected: %v", err)
	}
	err = validateLifecycleStateData(h, nested("mock", `{"bad":1}`))
	var he *HarnessError
	if !errors.As(err, &he) || !strings.Contains(err.Error(), "Lifecycle state failed schema validation for harness 'mock': bad data") {
		t.Fatalf("nested schema failure not wrapped: %v", err)
	}
}

// TS HarnessAgentSession.destroy awaits doDestroy() and swallows its
// rejection: teardown is best-effort and the session still ends destroyed.
func TestAgentSessionDestroySwallowsAdapterFailure(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{script: func(func(string, interface{})) []StreamPart { return nil }})
	called := false
	mock.session.(*mockSession).doDestroy = func(context.Context) error {
		called = true
		return errors.New("runtime already gone")
	}
	_, session := newTestAgent(t, mock, nil, Callbacks{}, nil)

	if err := session.Destroy(context.Background()); err != nil {
		t.Fatalf("Destroy returned %v, want nil (TS swallows doDestroy failures)", err)
	}
	if !called {
		t.Fatal("DoDestroy was not called")
	}
	if got := session.sessionState; got != SessionStateDestroyed {
		t.Fatalf("state = %v, want destroyed", got)
	}
}

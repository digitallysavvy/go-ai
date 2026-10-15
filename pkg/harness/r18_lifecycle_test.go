package harness

import (
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

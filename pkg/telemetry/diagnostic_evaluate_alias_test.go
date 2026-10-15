package telemetry

import "testing"

// The pre-rename DiagnosticEventOnEvaluate* constants stay for v0.5.2 callers
// and name the decide events.
func TestDeprecatedEvaluateDiagnosticEventAliases(t *testing.T) {
	cases := []struct{ old, current DiagnosticEventType }{
		{DiagnosticEventOnEvaluateStart, DiagnosticEventOnDecideStart},
		{DiagnosticEventOnEvaluateEnd, DiagnosticEventOnDecideEnd},
		{DiagnosticEventOnEvaluationModelCallStart, DiagnosticEventOnDecisionModelCallStart},
		{DiagnosticEventOnEvaluationModelCallEnd, DiagnosticEventOnDecisionModelCallEnd},
	}
	for _, c := range cases {
		if c.old != c.current {
			t.Errorf("%q != %q", c.old, c.current)
		}
	}
}

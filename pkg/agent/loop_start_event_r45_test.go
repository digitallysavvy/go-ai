package agent

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// A nil *TelemetrySettings records inputs, as the integrations treat it and
// as TS recordInputs defaults to true; an explicit RecordInputs false omits
// the system prompt.
func TestLoopStartEventRecordsSystemWithoutTelemetryOptions(t *testing.T) {
	a := NewToolLoopAgent(AgentConfig{Model: &testutil.MockLanguageModel{}, System: "be brief"})
	if e := a.loopStartEvent("c", nil, nil); e.System != "be brief" {
		t.Fatalf("nil settings: System = %q", e.System)
	}
	if e := a.loopStartEvent("c", &ai.TelemetrySettings{}, nil); e.System != "" {
		t.Fatalf("RecordInputs false: System = %q", e.System)
	}
}

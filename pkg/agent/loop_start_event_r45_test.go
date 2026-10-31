package agent

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// A nil *TelemetrySettings records inputs, as the integrations treat it and
// as TS recordInputs defaults to true; an explicit RecordInputs false omits
// the system prompt; a Settings literal that leaves it unset records.
func TestLoopStartEventRecordsSystemWithoutTelemetryOptions(t *testing.T) {
	a := NewToolLoopAgent(AgentConfig{Model: &testutil.MockLanguageModel{}, System: "be brief"})
	if e := a.loopStartEvent("c", nil, configuredCallConfig(a)); e.System != "be brief" {
		t.Fatalf("nil settings: System = %q", e.System)
	}
	if e := a.loopStartEvent("c", &ai.TelemetrySettings{}, configuredCallConfig(a)); e.System != "be brief" {
		t.Fatalf("unset RecordInputs: System = %q", e.System)
	}
	if e := a.loopStartEvent("c", &ai.TelemetrySettings{RecordInputs: telemetry.Bool(false)}, configuredCallConfig(a)); e.System != "" {
		t.Fatalf("RecordInputs false: System = %q", e.System)
	}
}

// configuredCallConfig is the call configuration of a without a PrepareCall
// override applied by the caller (its own PrepareCall, if any, runs).
func configuredCallConfig(a *ToolLoopAgent) PrepareCallConfig {
	return a.prepareCallConfig(context.Background(), 0, "", nil, nil, types.Usage{}, nil)
}

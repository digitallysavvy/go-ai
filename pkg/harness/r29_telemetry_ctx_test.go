package harness

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// TS filterIncludedContext/filterToolsContext always return an object ({}),
// never undefined, so telemetry events never carry a nil context map.
func TestTelemetryContextFiltersReturnEmptyObjectsNotNil(t *testing.T) {
	s := &telemetry.Options{IncludeToolsContext: map[string]map[string]bool{"t": {"a": true}}}
	if got := telemetryRuntimeContext(s, nil); got == nil {
		t.Error("runtime context must be {} not nil")
	}
	if got := telemetryToolContext(s, "t", nil); got == nil {
		t.Error("tool context must be {} not nil")
	}
	got := telemetryToolsContext(s, map[string]interface{}{"t": map[string]interface{}{"b": 1}})
	if v, ok := got["t"]; !ok || v == nil {
		t.Errorf("per-tool entry must be an empty object, got %#v", got)
	}
	if got := telemetryToolsContext(s, nil); got == nil {
		t.Error("tools context must be {} not nil")
	}
	if got := telemetryToolsContext(nil, map[string]interface{}{"t": 1}); got == nil {
		t.Error("tools context without settings must be {}")
	}
}

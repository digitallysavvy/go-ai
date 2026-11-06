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
	// A non-object or empty tool context is filtered to {}, not null.
	for _, ctxValue := range []interface{}{"not-an-object", map[string]interface{}{}} {
		got := telemetryToolsContext(s, map[string]interface{}{"t": ctxValue})
		if v, ok := got["t"]; !ok || v == nil {
			t.Errorf("per-tool entry for %#v must be an empty object, got %#v", ctxValue, got)
		}
	}
	if got := telemetryRuntimeContext(nil, map[string]interface{}{"a": 1}); got == nil {
		t.Error("runtime context without settings must be {}")
	}
}

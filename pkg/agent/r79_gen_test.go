package agent

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// SensitiveRuntimeContext omits the runtime context from every telemetry
// event, as GenerateText does: the legacy loop's start, step end and end
// events recorded the IncludeRuntimeContext keys regardless of it.
func TestR79GenLegacyLoopSensitiveRuntimeContext(t *testing.T) {
	full := map[string]interface{}{"userId": "user-123", "requestId": "request-123"}
	run := func(t *testing.T, configSensitive, callSensitive bool, prepare func(context.Context, PrepareCallConfig) PrepareCallConfig) []interface{} {
		t.Helper()
		rec := &r72agentRecorder{}
		var rcs []interface{}
		a := NewToolLoopAgent(AgentConfig{
			Model:                   r72agentTextModel("Hello!"),
			RuntimeContext:          full,
			SensitiveRuntimeContext: configSensitive,
			PrepareCall:             prepare,
			Telemetry: &ai.TelemetrySettings{
				IncludeRuntimeContext: map[string]bool{"requestId": true},
				Integrations:          []telemetry.TelemetryIntegration{&r72agentIntegration{rec: rec, rcs: &rcs}},
			},
		})
		if _, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "test", SensitiveRuntimeContext: callSensitive}); err != nil {
			t.Fatal(err)
		}
		return rcs
	}
	empty := map[string]interface{}{}
	for name, tc := range map[string]struct {
		config, call bool
		prepare      func(context.Context, PrepareCallConfig) PrepareCallConfig
	}{
		"config":      {config: true},
		"call option": {call: true},
		"prepareCall": {prepare: func(_ context.Context, c PrepareCallConfig) PrepareCallConfig {
			c.SensitiveRuntimeContext = true
			return c
		}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := run(t, tc.config, tc.call, tc.prepare); !reflect.DeepEqual(got, []interface{}{empty, empty, empty}) {
				t.Fatalf("telemetry runtime contexts = %v, want three empty", got)
			}
		})
	}
	t.Run("not sensitive", func(t *testing.T) {
		filtered := map[string]interface{}{"requestId": "request-123"}
		if got := run(t, false, false, nil); !reflect.DeepEqual(got, []interface{}{filtered, filtered, filtered}) {
			t.Fatalf("telemetry runtime contexts = %v", got)
		}
	})
}

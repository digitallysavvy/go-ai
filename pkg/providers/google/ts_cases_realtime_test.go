package google

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestTSCasesRealtimeParseServerEvent replays the parseServerEvent cases of
// realtime/google-realtime-event-mapper.test.ts (one fresh mapper per case).
func TestTSCasesRealtimeParseServerEvent(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/realtime_parse_event.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		raw, _ := json.Marshal(args[0])
		mapper := &googleRealtimeEventMapper{}
		events := mapper.parseServerEvent(raw)
		switch len(events) {
		case 0:
			return nil, nil, nil
		case 1:
			return events[0], nil, nil
		}
		return events, nil, nil
	}, nil)
}

// TestTSCasesRealtimeSessionConfig replays the buildGoogleSessionConfig cases
// of realtime/google-realtime-event-mapper.test.ts.
func TestTSCasesRealtimeSessionConfig(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/realtime_session_config.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		var cfg provider.RealtimeSessionConfig
		if m, ok := args[0].(map[string]interface{}); ok && m["__undef"] == nil {
			b, _ := json.Marshal(m)
			if err := json.Unmarshal(b, &cfg); err != nil {
				return nil, nil, err
			}
		}
		modelID, _ := args[1].(string)
		model := NewRealtimeModel(New(Config{APIKey: "k"}), modelID)
		return model.BuildSessionConfig(cfg), nil, nil
	}, nil)
}

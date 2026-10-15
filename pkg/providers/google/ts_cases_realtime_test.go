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

// TestTSCasesRealtimeSerializeClientEvent replays the serializeClientEvent
// cases of realtime/google-realtime-event-mapper.test.ts.
func TestTSCasesRealtimeSerializeClientEvent(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/realtime_serialize_event.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		b, _ := json.Marshal(args[0])
		var event provider.RealtimeClientEvent
		if err := json.Unmarshal(b, &event); err != nil {
			return nil, nil, err
		}
		modelID, _ := args[1].(string)
		model := NewRealtimeModel(New(Config{APIKey: "k"}), modelID)
		raw, err := model.SerializeClientEvent(event)
		if err != nil {
			return nil, nil, err
		}
		var out interface{}
		_ = json.Unmarshal(raw, &out)
		return out, nil, nil
	}, nil)
}

// TestTSCasesRealtimeStateful replays every parseServerEvent and
// serializeClientEvent call of realtime/google-realtime-event-mapper.test.ts
// in order, on the same mapper instance the TS test used, so the id counters
// and transcription accumulation that depend on earlier events are covered
// (testdata/tscases/realtime_stateful.jsonl.gz, recorded by
// pkg/internal/tsfixture/record).
func TestTSCasesRealtimeStateful(t *testing.T) {
	tests := tsfixture.LoadWire(t, "testdata/tscases/realtime_stateful.jsonl.gz")
	if len(tests) == 0 {
		t.Fatal("no recorded tests")
	}
	for _, w := range tests {
		w := w
		t.Run(w.Test, func(t *testing.T) {
			mappers := map[int]*googleRealtimeEventMapper{}
			for i, c := range w.Calls {
				m := mappers[c.Model.Inst]
				if m == nil {
					m = &googleRealtimeEventMapper{inputAudioRate: 16000}
					mappers[c.Model.Inst] = m
				}
				var got interface{}
				switch c.Method {
				case "parseServerEvent":
					raw, _ := json.Marshal(c.Args[0])
					events := m.parseServerEvent(raw)
					switch len(events) {
					case 0:
						got = nil
					case 1:
						got = roundTrip(t, events[0])
					default:
						got = roundTrip(t, events)
					}
				case "serializeClientEvent":
					b, _ := json.Marshal(c.Args[0])
					var event provider.RealtimeClientEvent
					if err := json.Unmarshal(b, &event); err != nil {
						t.Fatalf("call %d: %v", i, err)
					}
					modelID, _ := c.Args[1].(string)
					raw, err := m.serializeClientEvent(event, modelID)
					if err != nil {
						t.Fatalf("call %d: %v", i, err)
					}
					if raw == nil {
						got = nil
					} else {
						_ = json.Unmarshal(raw, &got)
					}
				}
				if d := tsfixture.DiffValues(c.Result, got); d != "" {
					t.Errorf("call %d (%s) differs from TS (- TS, + Go):\n%s", i, c.Method, d)
				}
			}
		})
	}
}

func roundTrip(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

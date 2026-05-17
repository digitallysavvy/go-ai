package ai

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

func TestTelemetryContextFiltering(t *testing.T) {
	settings := &TelemetrySettings{
		IncludeRuntimeContext: map[string]bool{"tenant": true, "ignored": false},
		IncludeToolsContext: map[string]map[string]bool{
			"weather": {"city": true},
		},
	}

	runtime := telemetryRuntimeContext(settings, map[string]interface{}{
		"tenant": "acme",
		"env":    "prod",
	})
	if len(runtime) != 1 || runtime["tenant"] != "acme" {
		t.Fatalf("runtime filter mismatch: %#v", runtime)
	}

	tools := telemetryToolsContext(settings, map[string]interface{}{
		"weather": map[string]interface{}{"city": "Paris", "zip": "75000"},
		"clock":   map[string]interface{}{"tz": "UTC"},
	})
	if len(tools) != 1 {
		t.Fatalf("tools context mismatch: %#v", tools)
	}
	weather := tools["weather"].(map[string]interface{})
	if len(weather) != 1 || weather["city"] != "Paris" {
		t.Fatalf("tool-specific filter mismatch: %#v", weather)
	}

	single := telemetryToolContext(settings, "weather", map[string]interface{}{"city": "Berlin", "zip": "10115"})
	if len(single) != 1 || single["city"] != "Berlin" {
		t.Fatalf("single tool context mismatch: %#v", single)
	}
}

func TestContextAsMapSupportsStringKeyedMapsAndPointers(t *testing.T) {
	type custom map[string]int
	raw := custom{"a": 1}
	out := contextAsMap(raw)
	if !reflect.DeepEqual(out, map[string]interface{}{"a": 1}) {
		t.Fatalf("contextAsMap custom map mismatch: %#v", out)
	}

	ptr := &raw
	out = contextAsMap(ptr)
	if !reflect.DeepEqual(out, map[string]interface{}{"a": 1}) {
		t.Fatalf("contextAsMap pointer map mismatch: %#v", out)
	}

	if got := contextAsMap((*custom)(nil)); got != nil {
		t.Fatalf("nil pointer should return nil, got %#v", got)
	}
	if got := contextAsMap(map[int]string{1: "x"}); got != nil {
		t.Fatalf("non-string map keys should return nil, got %#v", got)
	}
	if got := contextAsMap("not-a-map"); got != nil {
		t.Fatalf("non-map should return nil, got %#v", got)
	}

	if got := filterIncludedContext(nil, map[string]bool{"x": true}); len(got) != 0 {
		t.Fatalf("nil context should produce empty map: %#v", got)
	}
}

func TestSettingsIncludeRuntimeNilSafe(t *testing.T) {
	if settingsIncludeRuntime(nil) != nil {
		t.Fatal("settingsIncludeRuntime(nil) should be nil")
	}
}

func TestTelemetryRuntimeContextWithSensitivity(t *testing.T) {
	settings := &TelemetrySettings{
		IncludeRuntimeContext: map[string]bool{"tenant": true},
	}
	got := telemetryRuntimeContextWithSensitivity(settings, map[string]interface{}{"tenant": "acme"}, true)
	if len(got) != 0 {
		t.Fatalf("sensitive runtime context should be excluded, got %#v", got)
	}
	got = telemetryRuntimeContextWithSensitivity(settings, map[string]interface{}{"tenant": "acme"}, false)
	if got["tenant"] != "acme" {
		t.Fatalf("runtime context should be present when not sensitive, got %#v", got)
	}
}

var _ = telemetry.Bool

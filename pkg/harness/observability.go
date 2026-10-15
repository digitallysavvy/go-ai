package harness

import (
	"fmt"
	"os"
	"strings"
)

// This file mirrors TS `agent/observability/types.ts` and
// `agent/internal/resolve-observability.ts`.
//
// The host-facing diagnostic types are the same shapes the adapters emit, so
// Diagnostic and DebugConfig (spec.go) double as TS's `HarnessDiagnostic` and
// `HarnessDebugConfig`, and DebugLevel as `HarnessDebugLevel`.

// DiagnosticConsumer is implemented by a telemetry integration that also
// wants the per-line diagnostics stream. When diagnostics are enabled the
// agent calls IngestDiagnostic for every forwarded bridge diagnostic, in
// addition to driving the standard telemetry span lifecycle, so a single
// reporter registered in AgentSettings.Telemetry.Integrations (for example
// the one NewFileReporter returns) receives both spans and logs. Mirrors TS
// `HarnessDiagnosticConsumer`.
type DiagnosticConsumer interface {
	IngestDiagnostic(Diagnostic)
}

var envTruthy = map[string]struct{}{"1": {}, "true": {}, "yes": {}, "on": {}}

func parseEnvList(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// resolvedDebugConfig is the effective diagnostics configuration after env
// var fallbacks are applied.
type resolvedDebugConfig struct {
	Enabled    bool
	Level      DebugLevel
	Subsystems []string
}

// resolveDebugConfig resolves the effective debug config from explicit
// settings with env-var fallbacks. HARNESS_DEBUG, HARNESS_DEBUG_LEVEL and
// HARNESS_DEBUG_SUBSYSTEMS only fill fields the caller left unset: code is
// always authoritative. Mirrors TS `resolveDebugConfig`.
func resolveDebugConfig(debug *DebugConfig, getenv func(string) string) resolvedDebugConfig {
	var out resolvedDebugConfig
	if debug != nil && debug.Enabled != nil {
		out.Enabled = *debug.Enabled
	} else {
		_, out.Enabled = envTruthy[strings.ToLower(getenv("HARNESS_DEBUG"))]
	}
	switch {
	case debug != nil && debug.Level != "":
		out.Level = debug.Level
	case DebugLevel(getenv("HARNESS_DEBUG_LEVEL")).Valid():
		out.Level = DebugLevel(getenv("HARNESS_DEBUG_LEVEL"))
	default:
		out.Level = DebugLevelDebug
	}
	if debug != nil && debug.Subsystems != nil {
		out.Subsystems = append([]string{}, debug.Subsystems...) // non-nil: TS keeps an explicit []
	} else if l := parseEnvList(getenv("HARNESS_DEBUG_SUBSYSTEMS")); len(l) > 0 {
		out.Subsystems = l
	}
	return out
}

func formatDiagnosticForStderr(d Diagnostic) string {
	parts := []string{fmt.Sprintf("[harness:%s]", d.Level)}
	if d.Subsystem != "" {
		parts = append(parts, d.Subsystem)
	}
	if d.Message != "" {
		parts = append(parts, d.Message)
	}
	line := strings.Join(parts, " ")
	if d.Error != nil {
		name := d.Error.Name
		if name == "" {
			name = "Error"
		}
		line += fmt.Sprintf(" (%s: %s)", name, d.Error.Message)
	}
	return line + "\n"
}

// buildObservability builds the per-session Observability handle the agent
// hands to DoStart. It returns nil when diagnostics are disabled. When
// enabled, Report fans each adapter-emitted Diagnostic out, non-lossy, to the
// HARNESS_DEBUG stderr default, AgentSettings.OnLog, and every integration in
// AgentSettings.Telemetry.Integrations that implements DiagnosticConsumer.
// Mirrors TS `buildObservability`.
func buildObservability(settings AgentSettings) *Observability {
	resolved := resolveDebugConfig(settings.Debug, os.Getenv)
	if !resolved.Enabled {
		return nil
	}
	var consumers []DiagnosticConsumer
	if settings.Telemetry != nil {
		for _, integration := range settings.Telemetry.Integrations {
			if c, ok := integration.(DiagnosticConsumer); ok {
				consumers = append(consumers, c)
			}
		}
	}
	onLog := settings.OnLog
	enabled := true
	debug := &DebugConfig{Enabled: &enabled, Level: resolved.Level}
	if resolved.Subsystems != nil {
		debug.Subsystems = resolved.Subsystems
	}
	return &Observability{
		Debug: debug,
		Report: func(d Diagnostic) {
			// Never let the stderr default break the turn.
			_, _ = fmt.Fprint(os.Stderr, formatDiagnosticForStderr(d))
			if onLog != nil {
				onLog(d)
			}
			for _, c := range consumers {
				c.IngestDiagnostic(d)
			}
		},
	}
}

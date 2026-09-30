package ai

import (
	"bytes"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from packages/ai/src/logger/log-warnings.test.ts (ai@7.0.113).
// The global logger is process state, so these tests are not parallel.

func setupLogWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	t.Setenv("AI_SDK_LOG_WARNINGS", "")
	var buf bytes.Buffer
	resetLogWarningsState(&buf)
	t.Cleanup(func() { resetLogWarningsState(nil) })
	return &buf
}

func logLines(buf *bytes.Buffer) []string {
	s := strings.TrimRight(buf.String(), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func TestLogWarnings_Disabled(t *testing.T) {
	// "when AI_SDK_LOG_WARNINGS is false" > should not log any warnings (single/multiple),
	// should not count empty arrays as first call
	buf := setupLogWarnings(t)
	DisableLogWarnings()
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{{Type: "other", Message: "Test warning"}}, Provider: "providerX", Model: "modelY"})
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{{Type: "other", Message: "1"}, {Type: "other", Message: "2"}}, Provider: "p", Model: "m"})
	LogWarnings(LogWarningsOptions{Warnings: nil, Provider: "p", Model: "m"})
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestLogWarnings_DisabledByEnv(t *testing.T) {
	buf := setupLogWarnings(t)
	t.Setenv("AI_SDK_LOG_WARNINGS", "false")
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{{Type: "other", Message: "x"}}})
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestLogWarnings_CustomFunction(t *testing.T) {
	// "when AI_SDK_LOG_WARNINGS is a custom function"
	buf := setupLogWarnings(t)
	var calls []LogWarningsOptions
	SetLogWarnings(func(o LogWarningsOptions) { calls = append(calls, o) })

	opts := LogWarningsOptions{
		Warnings: []types.Warning{
			{Type: "unsupported", Feature: "temperature", Details: "Temperature not supported"},
			{Type: "other", Message: "Another warning"},
		},
		Provider: "provider", Model: "model",
	}
	LogWarnings(opts)
	LogWarnings(LogWarningsOptions{Provider: "x", Model: "y"}) // empty: not called

	if len(calls) != 1 || len(calls[0].Warnings) != 2 || calls[0].Provider != "provider" || calls[0].Model != "model" {
		t.Fatalf("unexpected custom logger calls: %+v", calls)
	}
	if buf.Len() != 0 {
		t.Fatalf("default output must not be written with custom logger (no info note), got %q", buf.String())
	}
}

func TestLogWarnings_DefaultWritesBannerOnceThenWarnings(t *testing.T) {
	// "should emit the information note and warning ... without logging to stdout",
	// "should only emit the information note on the first non-empty call",
	// "should only log for non-empty warnings"
	buf := setupLogWarnings(t)
	LogWarnings(LogWarningsOptions{Provider: "err", Model: "m"})
	if buf.Len() != 0 {
		t.Fatalf("empty warnings must not log, got %q", buf.String())
	}
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{{Type: "other", Message: "1"}}, Provider: "a", Model: "b"})
	LogWarnings(LogWarningsOptions{Warnings: nil, Provider: "a", Model: "b"})
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{{Type: "other", Message: "2"}}, Provider: "a", Model: "b"})

	want := []string{
		FirstWarningInfoMessage,
		"AI SDK Warning (a / b): 1",
		"AI SDK Warning (a / b): 2",
	}
	got := logLines(buf)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestLogWarnings_RestoreDefaultAfterDisable(t *testing.T) {
	buf := setupLogWarnings(t)
	DisableLogWarnings()
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{{Type: "other", Message: "hidden"}}})
	SetLogWarnings(nil)
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{{Type: "other", Message: "shown"}}})
	got := logLines(buf)
	if len(got) != 2 || got[0] != FirstWarningInfoMessage || got[1] != "AI SDK Warning: shown" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatWarning(t *testing.T) {
	// "should handle various warning types per formatWarning" + 008271d deprecated case
	tests := []struct {
		name     string
		warning  types.Warning
		provider string
		model    string
		want     string
	}{
		{"unsupported", types.Warning{Type: "unsupported", Feature: "mediaType", Details: "detail"}, "zzz", "MMM",
			`AI SDK Warning (zzz / MMM): The feature "mediaType" is not supported. detail`},
		{"unsupported no details", types.Warning{Type: "unsupported", Feature: "voice"}, "zzz", "MMM",
			`AI SDK Warning (zzz / MMM): The feature "voice" is not supported.`},
		{"unsupported legacy message", types.Warning{Type: "unsupported", Feature: "voice", Message: "detail2"}, "zzz", "MMM",
			`AI SDK Warning (zzz / MMM): The feature "voice" is not supported. detail2`},
		{"compatibility", types.Warning{Type: "compatibility", Feature: "tools", Details: "emulated"}, "p", "m",
			`AI SDK Warning (p / m): The feature "tools" is used in a compatibility mode. emulated`},
		{"deprecated", types.Warning{Type: "deprecated", Setting: "providerOptions key 'old-key'", Message: "Use 'oldKey' instead."}, "zzz", "MMM",
			`AI SDK Warning (zzz / MMM): Deprecated: "providerOptions key 'old-key'". Use 'oldKey' instead.`},
		{"other", types.Warning{Type: "other", Message: "other msg"}, "zzz", "MMM",
			`AI SDK Warning (zzz / MMM): other msg`},
		{"unknown provider/model", types.Warning{Type: "other", Message: "messx"}, "unknown provider", "unknown model",
			`AI SDK Warning (unknown provider / unknown model): messx`},
		{"no scope", types.Warning{Type: "other", Message: "x"}, "p", "",
			`AI SDK Warning: x`},
		{"unknown type", types.Warning{Type: "mystery", Message: "m"}, "", "",
			"AI SDK Warning: {\n  \"type\": \"mystery\",\n  \"message\": \"m\"\n}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatWarning(tt.warning, tt.provider, tt.model); got != tt.want {
				t.Fatalf("got %q\nwant %q", got, tt.want)
			}
		})
	}
}

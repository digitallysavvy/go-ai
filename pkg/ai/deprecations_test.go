package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Ports packages/ai/src/logger/deprecations.test.ts (TS #22182).
func TestGetDeprecationCode(t *testing.T) {
	registered := map[string]string{
		"generateObject":                            "AISDK_DEP_GENERATE_OBJECT",
		"streamObject":                              "AISDK_DEP_STREAM_OBJECT",
		"experimental_generateSpeech":               "AISDK_DEP_EXPERIMENTAL_GENERATE_SPEECH",
		"experimental_transcribe":                   "AISDK_DEP_EXPERIMENTAL_TRANSCRIBE",
		`"image" content part`:                      "AISDK_DEP_IMAGE_CONTENT_PART",
		"rawInput in output-error UI message parts": "AISDK_DEP_UI_MESSAGE_RAW_INPUT",
	}
	for _, typ := range []string{"file-data", "file-url", "file-id", "file-reference", "image-data", "image-url", "image-file-id", "image-file-reference"} {
		registered[`"tool-result" content of type "`+typ+`"`] = "AISDK_DEP_TOOL_RESULT_" + strings.ToUpper(strings.ReplaceAll(typ, "-", "_"))
	}
	for setting, code := range registered {
		if got := GetDeprecationCode(setting, ""); got != code {
			t.Errorf("GetDeprecationCode(%q) = %q, want %q", setting, got, code)
		}
	}

	if got := GetDeprecationCode("generateObject", "test"); got != "AISDK_DEP_PROVIDER_test__generateObject" {
		t.Errorf("provider-scoped code = %q", got)
	}

	inputs := [][2]string{
		{"old-key", "test"}, {"old_key", "test"}, {"old_002Dkey", "test"}, {"Old-key", "test"},
		{"old-key", "other"}, {"old-key", ""}, {"key", "test__old"}, {"old__key", "test"},
		{"😀", ""}, {"😁", ""}, {"toString", ""},
	}
	seen := map[string]bool{}
	for _, in := range inputs {
		code := GetDeprecationCode(in[0], in[1])
		if seen[code] {
			t.Errorf("code %q collides", code)
		}
		seen[code] = true
		for _, r := range code {
			if r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
				t.Errorf("code %q has invalid char %q", code, r)
			}
		}
	}
	if got := GetDeprecationCode("old-key", "test"); got != "AISDK_DEP_PROVIDER_test__old_002Dkey" {
		t.Errorf("got %q", got)
	}
	if got := GetDeprecationCode("😀", ""); got != "AISDK_DEP_SETTING__D83D_DE00" {
		t.Errorf("surrogate pair encoding = %q", got)
	}
}

func TestLogWarnings_DeprecationsAreCodedAndEmittedOncePerCode(t *testing.T) {
	buf := setupLogWarnings(t)
	w := types.Warning{Type: "deprecated", Setting: "generateObject", Message: "Use generateText with an output setting instead."}
	other := types.Warning{Type: "other", Message: "Repeated warning."}

	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{w, w}})
	w2 := w
	w2.Message = "Updated wording."
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{w2}})
	w3 := w
	w3.Setting = "streamObject"
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{w3}})
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{w, other}})
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{w, other}})

	lines := logLines(buf)
	want := []string{
		FirstWarningInfoMessage,
		`[AISDK_DEP_GENERATE_OBJECT] AI SDK Warning: Deprecated: "generateObject". Use generateText with an output setting instead.`,
		`[AISDK_DEP_STREAM_OBJECT] AI SDK Warning: Deprecated: "streamObject". Use generateText with an output setting instead.`,
		"AI SDK Warning: Repeated warning.",
		"AI SDK Warning: Repeated warning.",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestLogWarnings_DeprecationsDedupeAcrossModelsButNotProviders(t *testing.T) {
	buf := setupLogWarnings(t)
	w := types.Warning{Type: "deprecated", Setting: "generateObject", Message: "m"}
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{w}, Provider: "a", Model: "first"})
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{w}, Provider: "a", Model: "second"})
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{w}, Provider: "b", Model: "first"})
	lines := logLines(buf)
	if len(lines) != 3 || !strings.HasPrefix(lines[2], "[AISDK_DEP_PROVIDER_b__generateObject] ") {
		t.Fatalf("lines = %v", lines)
	}
}

func TestLogWarnings_DeprecationCodesNotConsumedWhenDisabledOrCustom(t *testing.T) {
	buf := setupLogWarnings(t)
	w := types.Warning{Type: "deprecated", Setting: "generateObject", Message: "m"}

	DisableLogWarnings()
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{w}})
	var got []LogWarningsOptions
	SetLogWarnings(func(o LogWarningsOptions) { got = append(got, o) })
	opts := LogWarningsOptions{Warnings: []types.Warning{w, w}}
	LogWarnings(opts)
	LogWarnings(opts)
	if len(got) != 2 || len(got[0].Warnings) != 2 {
		t.Fatalf("custom logger must see every warning, got %v", got)
	}
	SetLogWarnings(nil)
	LogWarnings(LogWarningsOptions{Warnings: []types.Warning{w}})
	if lines := logLines(buf); len(lines) != 2 {
		t.Fatalf("default logger should still emit the code once, got %v", lines)
	}
}

// TS #22182: experimental_transcribe warns, like experimental_generateSpeech.
func TestExperimentalTranscribe_LogsDeprecationWarning(t *testing.T) {
	buf := setupLogWarnings(t)
	model := &testutil.MockTranscriptionModel{
		DoTranscribeFunc: func(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
			return &types.TranscriptionResult{Text: "hi"}, nil
		},
	}
	for i := 0; i < 2; i++ {
		if _, err := ExperimentalTranscribe(context.Background(), TranscribeOptions{Model: model, Audio: []byte("audio")}); err != nil {
			t.Fatal(err)
		}
	}
	lines := logLines(buf)
	if len(lines) != 2 || lines[1] != `[AISDK_DEP_EXPERIMENTAL_TRANSCRIBE] AI SDK Warning: Deprecated: "experimental_transcribe". Use transcribe instead.` {
		t.Fatalf("lines = %v", lines)
	}
}

package ai

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// LogWarningsOptions is the input of a LogWarningsFunction.
// Mirrors the options object of the TypeScript SDK's LogWarningsFunction.
type LogWarningsOptions struct {
	// Warnings are the warnings returned by the model provider.
	Warnings []types.Warning

	// Provider is the provider id used for the call, if scoped to a provider.
	Provider string

	// Model is the model id used for the call, if scoped to a provider.
	Model string
}

// LogWarningsFunction is a function for logging warnings. Install one with
// SetLogWarnings to replace the default stderr logger.
type LogWarningsFunction func(options LogWarningsOptions)

// FirstWarningInfoMessage is written once, before the first logged warning,
// by the default logger. Mirrors the TS FIRST_WARNING_INFO_MESSAGE; the Go
// equivalent of the AI_SDK_LOG_WARNINGS global is DisableLogWarnings /
// SetLogWarnings, or the AI_SDK_LOG_WARNINGS=false environment variable.
const FirstWarningInfoMessage = "AI SDK Warning System: To turn off warning logging, call ai.DisableLogWarnings() or set the AI_SDK_LOG_WARNINGS environment variable to false."

var logWarningsState = struct {
	mu             sync.Mutex
	disabled       bool
	custom         LogWarningsFunction
	hasLoggedBefore bool
	output         io.Writer
}{}

// SetLogWarnings installs a global warning logger, the Go analog of assigning
// a function to the TS `globalThis.AI_SDK_LOG_WARNINGS`. The function receives
// every non-empty batch of warnings instead of the default stderr output.
// Passing nil restores the default logger (and re-enables logging).
func SetLogWarnings(fn LogWarningsFunction) {
	logWarningsState.mu.Lock()
	defer logWarningsState.mu.Unlock()
	logWarningsState.custom = fn
	logWarningsState.disabled = false
}

// DisableLogWarnings turns warning logging off, the Go analog of setting the
// TS `globalThis.AI_SDK_LOG_WARNINGS = false`. Call SetLogWarnings(nil) to
// restore the default logger.
func DisableLogWarnings() {
	logWarningsState.mu.Lock()
	defer logWarningsState.mu.Unlock()
	logWarningsState.custom = nil
	logWarningsState.disabled = true
}

// LogWarnings logs provider warnings. Core functions (Embed, EmbedMany,
// Rerank, GenerateImage, GenerateSpeech, Transcribe, GenerateVideo, ...) call
// it with the warnings returned by the model.
//
// Behavior mirrors the TypeScript SDK's logWarnings:
//   - an empty warnings slice does nothing (and does not count as a first call);
//   - when disabled (DisableLogWarnings or AI_SDK_LOG_WARNINGS=false), nothing is logged;
//   - when a custom logger is installed (SetLogWarnings), it is called with the options;
//   - otherwise each warning is formatted as "AI SDK Warning (provider / model): ..."
//     and written to os.Stderr (never stdout), preceded once per process by
//     FirstWarningInfoMessage.
func LogWarnings(options LogWarningsOptions) {
	if len(options.Warnings) == 0 {
		return
	}

	logWarningsState.mu.Lock()
	if logWarningsState.disabled || strings.EqualFold(strings.TrimSpace(os.Getenv("AI_SDK_LOG_WARNINGS")), "false") {
		logWarningsState.mu.Unlock()
		return
	}
	if custom := logWarningsState.custom; custom != nil {
		logWarningsState.mu.Unlock()
		custom(options)
		return
	}
	defer logWarningsState.mu.Unlock()

	out := logWarningsState.output
	if out == nil {
		out = os.Stderr
	}
	if !logWarningsState.hasLoggedBefore {
		logWarningsState.hasLoggedBefore = true
		fmt.Fprintln(out, FirstWarningInfoMessage)
	}
	for _, warning := range options.Warnings {
		fmt.Fprintln(out, FormatWarning(warning, options.Provider, options.Model))
	}
}

// FormatWarning renders a warning the way the default logger does
// (TS formatWarning). The "(provider / model)" scope is included only when
// both provider and model are non-empty.
func FormatWarning(warning types.Warning, provider, model string) string {
	scope := ""
	if provider != "" && model != "" {
		scope = fmt.Sprintf(" (%s / %s)", provider, model)
	}
	prefix := "AI SDK Warning" + scope + ":"

	// Go warnings historically put free text in Message for all types; prefer
	// the TS field for each type and fall back to the other one.
	details := warning.Details
	if details == "" {
		details = warning.Message
	}
	message := warning.Message
	if message == "" {
		message = warning.Details
	}

	switch warning.Type {
	case "unsupported":
		out := fmt.Sprintf("%s The feature \"%s\" is not supported.", prefix, warning.Feature)
		if details != "" {
			out += " " + details
		}
		return out
	case "compatibility":
		out := fmt.Sprintf("%s The feature \"%s\" is used in a compatibility mode.", prefix, warning.Feature)
		if details != "" {
			out += " " + details
		}
		return out
	case "deprecated":
		return fmt.Sprintf("%s Deprecated: \"%s\". %s", prefix, warning.Setting, message)
	case "other":
		return fmt.Sprintf("%s %s", prefix, message)
	default:
		b, err := json.MarshalIndent(warning, "", "  ")
		if err != nil {
			return fmt.Sprintf("%s %+v", prefix, warning)
		}
		return fmt.Sprintf("%s %s", prefix, string(b))
	}
}

// logModelWarnings is the internal call-site helper.
func logModelWarnings(warnings []types.Warning, provider, model string) {
	LogWarnings(LogWarningsOptions{Warnings: warnings, Provider: provider, Model: model})
}

// resetLogWarningsState resets the logger to its initial state (tests only).
func resetLogWarningsState(output io.Writer) {
	logWarningsState.mu.Lock()
	defer logWarningsState.mu.Unlock()
	logWarningsState.disabled = false
	logWarningsState.custom = nil
	logWarningsState.hasLoggedBefore = false
	logWarningsState.output = output
}

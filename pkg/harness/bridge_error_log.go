package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// FormatBridgeError renders an error value for logs. Go errors render as
// their message; serialized bridge errors (a *DiagnosticError, or a JSON
// object with a string `message`) render as `stack`, else `name: message`;
// anything else is JSON-stringified. Mirrors TS `formatBridgeError`
// (39c8276).
func FormatBridgeError(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		return v
	case *DiagnosticError:
		if v != nil {
			return formatSerializedError(v.Name, v.Message, v.Stack)
		}
	case DiagnosticError:
		return formatSerializedError(v.Name, v.Message, v.Stack)
	case error:
		return v.Error()
	case map[string]any:
		if msg, ok := v["message"].(string); ok {
			name, _ := v["name"].(string)
			stack, _ := v["stack"].(string)
			return formatSerializedError(name, msg, stack)
		}
	case json.RawMessage:
		var decoded any
		if json.Unmarshal(v, &decoded) == nil {
			return FormatBridgeError(decoded)
		}
		return string(v)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(raw)
}

func formatSerializedError(name, message, stack string) string {
	if stack != "" {
		return stack
	}
	if name != "" {
		return name + ": " + message
	}
	return message
}

// LogBridgeErrorOptions is the input of LogBridgeError.
type LogBridgeErrorOptions struct {
	HarnessID string
	SessionID string
	Context   string
	Error     any
	// Write receives each formatted line (newline-terminated). Defaults to
	// os.Stderr.
	Write func(line string)
}

// LogBridgeError writes an error, one prefixed line per non-blank line:
// `[harness:<id>:error session=<sid>] <context>: <error>`. Mirrors TS
// `logBridgeError`.
func LogBridgeError(opts LogBridgeErrorOptions) {
	write := opts.Write
	if write == nil {
		write = func(line string) { _, _ = os.Stderr.WriteString(line) }
	}
	prefix := "[harness:" + opts.HarnessID + ":error"
	if opts.SessionID != "" {
		prefix += " session=" + opts.SessionID
	}
	prefix += "]"
	message := FormatBridgeError(opts.Error)
	if opts.Context != "" {
		message = opts.Context + ": " + message
	}
	for _, line := range strings.Split(message, "\n") {
		if providerutils.JSTrim(line) != "" {
			write(prefix + " " + line + "\n")
		}
	}
}

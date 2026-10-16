package responses

import (
	"encoding/json"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// The shell, local_shell, apply_patch and computer tool payloads are decoded
// with validateTypes in TS (shellInputSchema, localShellOutputSchema, ...), so
// a malformed payload throws a TypeValidationError instead of being coerced.
// The nodes below are those zod schemas; unknown keys are stripped (zod
// objects drop them when parsing).

var (
	optStringArray = zodlite.Optional(zodlite.Array(zodlite.String))
	optNumber      = zodlite.Optional(zodlite.Number)
	optString      = zodlite.Optional(zodlite.String)
)

var localShellInputNode = zodlite.Object(
	zodlite.F("action", zodlite.Typed("exec",
		zodlite.F("command", zodlite.Array(zodlite.String)),
		zodlite.F("timeoutMs", optNumber),
		zodlite.F("user", optString),
		zodlite.F("workingDirectory", optString),
		zodlite.F("env", zodlite.Optional(zodlite.RecordOf(zodlite.String))),
	)),
)

var localShellOutputNode = zodlite.Object(zodlite.F("output", zodlite.String))

var shellInputNode = zodlite.Object(
	zodlite.F("action", zodlite.Object(
		zodlite.F("commands", zodlite.Array(zodlite.String)),
		zodlite.F("timeoutMs", optNumber),
		zodlite.F("maxOutputLength", optNumber),
	)),
)

var shellOutputNode = zodlite.Object(
	zodlite.F("output", zodlite.Array(zodlite.Object(
		zodlite.F("stdout", zodlite.String),
		zodlite.F("stderr", zodlite.String),
		zodlite.F("outcome", zodlite.Disc(map[string]zodlite.Node{
			"timeout": zodlite.Typed("timeout"),
			"exit":    zodlite.Typed("exit", zodlite.F("exitCode", zodlite.Number)),
		})),
	))),
)

var applyPatchInputNode = zodlite.Object(
	zodlite.F("callId", zodlite.String),
	zodlite.F("operation", zodlite.Disc(map[string]zodlite.Node{
		"create_file": zodlite.Typed("create_file", zodlite.F("path", zodlite.String), zodlite.F("diff", zodlite.String)),
		"delete_file": zodlite.Typed("delete_file", zodlite.F("path", zodlite.String)),
		"update_file": zodlite.Typed("update_file", zodlite.F("path", zodlite.String), zodlite.F("diff", zodlite.String)),
	})),
)

var applyPatchOutputNode = zodlite.Object(
	zodlite.F("status", zodlite.Enum("completed", "failed")),
	zodlite.F("output", optString),
)

var computerSafetyCheckNode = zodlite.Object(
	zodlite.F("id", zodlite.String),
	zodlite.F("code", optString),
	zodlite.F("message", optString),
)

// computerActionFields lists, per action type, the keys zod keeps; the node
// validates them.
var computerActionFields = map[string][]string{
	"click":        {"type", "button", "x", "y", "keys"},
	"double_click": {"type", "x", "y", "keys"},
	"drag":         {"type", "path", "keys"},
	"keypress":     {"type", "keys"},
	"move":         {"type", "x", "y", "keys"},
	"screenshot":   {"type"},
	"scroll":       {"type", "x", "y", "scrollX", "scrollY", "keys"},
	"type":         {"type", "text"},
	"wait":         {"type"},
}

var computerActionNode = zodlite.Disc(map[string]zodlite.Node{
	"click": zodlite.Typed("click",
		zodlite.F("button", zodlite.Enum("left", "right", "wheel", "back", "forward")),
		zodlite.F("x", zodlite.Number), zodlite.F("y", zodlite.Number), zodlite.F("keys", optStringArray)),
	"double_click": zodlite.Typed("double_click",
		zodlite.F("x", zodlite.Number), zodlite.F("y", zodlite.Number), zodlite.F("keys", optStringArray)),
	"drag": zodlite.Typed("drag",
		zodlite.F("path", zodlite.Array(zodlite.Object(zodlite.F("x", zodlite.Number), zodlite.F("y", zodlite.Number)))),
		zodlite.F("keys", optStringArray)),
	"keypress": zodlite.Typed("keypress", zodlite.F("keys", zodlite.Array(zodlite.String))),
	"move": zodlite.Typed("move",
		zodlite.F("x", zodlite.Number), zodlite.F("y", zodlite.Number), zodlite.F("keys", optStringArray)),
	"screenshot": zodlite.Typed("screenshot"),
	"scroll": zodlite.Typed("scroll",
		zodlite.F("x", zodlite.Number), zodlite.F("y", zodlite.Number),
		zodlite.F("scrollX", zodlite.Number), zodlite.F("scrollY", zodlite.Number), zodlite.F("keys", optStringArray)),
	"type": zodlite.Typed("type", zodlite.F("text", zodlite.String)),
	"wait": zodlite.Typed("wait"),
})

var computerInputNode = zodlite.Object(
	zodlite.F("actions", zodlite.Array(computerActionNode)),
	zodlite.F("pendingSafetyChecks", zodlite.Array(computerSafetyCheckNode)),
	zodlite.F("status", zodlite.Enum("in_progress", "completed", "incomplete")),
)

func computerScreenshotNode(requiredKey string) zodlite.Node {
	optional := map[string]zodlite.Node{"imageUrl": zodlite.String, "fileId": zodlite.String}
	fields := []zodlite.Field{
		zodlite.F(requiredKey, optional[requiredKey]),
	}
	for _, k := range []string{"imageUrl", "fileId"} {
		if k != requiredKey {
			fields = append(fields, zodlite.F(k, zodlite.Optional(zodlite.String)))
		}
	}
	fields = append(fields, zodlite.F("detail", zodlite.Optional(zodlite.Enum("auto", "low", "high", "original"))))
	return zodlite.Typed("computer_screenshot", fields...)
}

var computerOutputNode = zodlite.Object(
	zodlite.F("output", zodlite.Union(computerScreenshotNode("imageUrl"), computerScreenshotNode("fileId"))),
	zodlite.F("acknowledgedSafetyChecks", zodlite.Optional(zodlite.Array(computerSafetyCheckNode))),
)

// validateToolPayload validates value against node and returns the TS
// TypeValidationError (nil when it matches).
func validateToolPayload(node zodlite.Node, value interface{}) error {
	data, err := jsonstr.Marshal(value)
	if err != nil {
		return &providererrors.TypeValidationError{Cause: err, Message: err.Error()}
	}
	msg := zodlite.ValidationMessage(node, string(data))
	if msg == "" {
		return nil
	}
	valueText := ""
	if _, after, ok := strings.Cut(msg, "Value: "); ok {
		valueText, _, _ = strings.Cut(after, ".\nError message: ")
	}
	return &providererrors.TypeValidationError{
		Context: &providererrors.ValidationContext{},
		Value:   valueText,
		Message: zodlite.IssueText(msg),
	}
}

// decodeStrict validates value against node, then decodes it into target.
func decodeStrict(node zodlite.Node, value interface{}, target interface{}) error {
	if err := validateToolPayload(node, value); err != nil {
		return err
	}
	data, err := jsonstr.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

package harness

import (
	"fmt"
	"sort"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// BuiltinToolName is a cross-harness common built-in tool name (TS
// `HarnessV1BuiltinToolName`).
type BuiltinToolName string

// Common built-in tool names, in TS declaration order.
const (
	BuiltinToolRead             BuiltinToolName = "read"
	BuiltinToolWrite            BuiltinToolName = "write"
	BuiltinToolEdit             BuiltinToolName = "edit"
	BuiltinToolBash             BuiltinToolName = "bash"
	BuiltinToolGrep             BuiltinToolName = "grep"
	BuiltinToolGlob             BuiltinToolName = "glob"
	BuiltinToolWebSearch        BuiltinToolName = "webSearch"
	BuiltinToolAskUserQuestions BuiltinToolName = "askUserQuestions"
)

// BuiltinToolNames mirrors TS `HARNESS_V1_BUILTIN_TOOL_NAMES` (declaration
// order).
var BuiltinToolNames = []BuiltinToolName{
	BuiltinToolRead,
	BuiltinToolWrite,
	BuiltinToolEdit,
	BuiltinToolBash,
	BuiltinToolGrep,
	BuiltinToolGlob,
	BuiltinToolWebSearch,
	BuiltinToolAskUserQuestions,
}

// BuiltinToolUseKind classifies a built-in tool for permission handling.
type BuiltinToolUseKind string

const (
	BuiltinToolUseKindReadonly BuiltinToolUseKind = "readonly"
	BuiltinToolUseKindEdit     BuiltinToolUseKind = "edit"
	BuiltinToolUseKindBash     BuiltinToolUseKind = "bash"
)

// BuiltinTool is a tool the adapter's runtime exposes natively: a types.Tool
// plus harness metadata. Mirrors TS `HarnessV1BuiltinTool`.
type BuiltinTool struct {
	types.Tool

	// NativeName is the runtime's own name, set only when the builtinTools key
	// is a common name.
	NativeName string
	// CommonName is the cross-harness label from BuiltinToolNames.
	CommonName BuiltinToolName
	// ToolUseKind classifies the tool for permission modes.
	ToolUseKind BuiltinToolUseKind
}

func objectSchema(required []string, props map[string]any) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}

func stringProps(names ...string) map[string]any {
	props := make(map[string]any, len(names))
	for _, name := range names {
		props[name] = map[string]any{"type": "string"}
	}
	return props
}

func standardTool(name BuiltinToolName, description string, required ...string) types.Tool {
	return types.Tool{
		Name:        string(name),
		Description: description,
		Parameters:  objectSchema(required, stringProps(required...)),
	}
}

// StandardBuiltinTools returns the cross-harness vocabulary of common built-in tools
// with their baseline JSON Schema inputs. Mirrors TS `HARNESS_V1_BUILTIN_TOOLS`.
// A fresh map is returned on every call.
func StandardBuiltinTools() map[BuiltinToolName]types.Tool {
	return map[BuiltinToolName]types.Tool{
		BuiltinToolRead:      standardTool(BuiltinToolRead, "Read file contents", "file_path"),
		BuiltinToolWrite:     standardTool(BuiltinToolWrite, "Write content to a file", "file_path", "content"),
		BuiltinToolEdit:      standardTool(BuiltinToolEdit, "Edit a file by replacing text", "file_path", "old_string", "new_string"),
		BuiltinToolBash:      standardTool(BuiltinToolBash, "Execute a shell command", "command"),
		BuiltinToolGrep:      standardTool(BuiltinToolGrep, "Search file contents with regex", "pattern"),
		BuiltinToolGlob:      standardTool(BuiltinToolGlob, "Find files matching a glob pattern", "pattern"),
		BuiltinToolWebSearch: standardTool(BuiltinToolWebSearch, "Search the web", "query"),
		BuiltinToolAskUserQuestions: {
			Name:         string(BuiltinToolAskUserQuestions),
			Description:  QuestionsToolDescription,
			Parameters:   QuestionsToolInputJSONSchema(),
			OutputSchema: QuestionsToolOutputJSONSchema(),
		},
	}
}

// CommonToolOptions configures CommonTool.
type CommonToolOptions struct {
	NativeName  string
	ToolUseKind BuiltinToolUseKind
	Description string
	// InputSchema is the adapter's JSON Schema. It must accept every input the
	// standard schema for the common name accepts: every property the
	// standard requires must be declared (extra optional fields are
	// encouraged).
	InputSchema map[string]any
}

// CommonTool declares a built-in tool that maps to a cross-harness common
// name. Mirrors TS `commonTool()`. TS enforces the superset rule at compile
// time; Go enforces it at construction and panics on violation (tool tables
// are package-level declarations, like regexp.MustCompile).
func CommonTool(commonName BuiltinToolName, opts CommonToolOptions) BuiltinTool {
	if err := checkCommonToolSuperset(commonName, opts.InputSchema); err != nil {
		panic(err)
	}
	return BuiltinTool{
		Tool: types.Tool{
			Name:        string(commonName),
			Description: opts.Description,
			Parameters:  opts.InputSchema,
		},
		NativeName:  opts.NativeName,
		CommonName:  commonName,
		ToolUseKind: opts.ToolUseKind,
	}
}

func checkCommonToolSuperset(commonName BuiltinToolName, schema map[string]any) error {
	standard, ok := StandardBuiltinTools()[commonName]
	if !ok {
		return fmt.Errorf("harness: unknown common tool name %q", commonName)
	}
	standardSchema, _ := standard.Parameters.(map[string]any)
	required := schemaRequired(standardSchema)
	props, _ := schema["properties"].(map[string]any)
	missing := make([]string, 0)
	for _, name := range required {
		if _, ok := props[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("harness: adapter input schema for %q must be a superset of the standard schema; missing %v", commonName, missing)
	}
	return nil
}

func schemaRequired(schema map[string]any) []string {
	switch req := schema["required"].(type) {
	case []string:
		return req
	case []any:
		out := make([]string, 0, len(req))
		for _, v := range req {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

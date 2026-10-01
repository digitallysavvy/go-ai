package providerutils

import (
	"encoding/json"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const (
	defaultJSONInstructionSchemaPrefix  = "JSON schema:"
	defaultJSONInstructionSchemaSuffix  = "You MUST answer with a JSON object that matches the JSON schema above."
	defaultJSONInstructionGenericSuffix = "You MUST answer with JSON."
)

// InjectJSONInstructionOptions customizes InjectJSONInstruction /
// InjectJSONInstructionIntoMessages. A zero value uses the TypeScript SDK's
// defaults.
type InjectJSONInstructionOptions struct {
	// SchemaPrefix precedes the serialized schema. Defaults to "JSON schema:"
	// when Schema is non-nil.
	SchemaPrefix string
	// SchemaSuffix follows the serialized schema (or, with no schema, the
	// whole instruction). Defaults to a JSON-schema-specific message when
	// Schema is non-nil, otherwise a generic "answer with JSON" message.
	SchemaSuffix string
}

// InjectJSONInstruction builds a JSON-mode instruction string appended to a
// prompt, mirroring the TypeScript SDK's injectJsonInstruction
// (provider-utils). Used by providers that need to fall back from native JSON
// schema/object modes to a plain-text instruction (e.g. Alibaba/Mistral JSON
// object mode). schema may be nil (no schema available) or an empty, non-nil
// map (an empty JSON Schema, which is still serialized and included).
func InjectJSONInstruction(prompt string, schema map[string]interface{}, opts InjectJSONInstructionOptions) string {
	hasSchema := schema != nil
	schemaPrefix := opts.SchemaPrefix
	if schemaPrefix == "" && hasSchema {
		schemaPrefix = defaultJSONInstructionSchemaPrefix
	}
	schemaSuffix := opts.SchemaSuffix
	if schemaSuffix == "" {
		if hasSchema {
			schemaSuffix = defaultJSONInstructionSchemaSuffix
		} else {
			schemaSuffix = defaultJSONInstructionGenericSuffix
		}
	}

	var lines []string
	if prompt != "" {
		lines = append(lines, prompt, "")
	}
	if schemaPrefix != "" {
		lines = append(lines, schemaPrefix)
	}
	if hasSchema {
		if b, err := json.Marshal(schema); err == nil {
			lines = append(lines, string(b))
		}
	}
	if schemaSuffix != "" {
		lines = append(lines, schemaSuffix)
	}
	return strings.Join(lines, "\n")
}

// InjectJSONInstructionIntoMessages mirrors the TypeScript SDK's
// injectJsonInstructionIntoMessages: it merges a JSON-mode instruction (and,
// when schema is non-nil, the serialized schema) into the leading system
// message, creating a new one when the prompt doesn't start with a system
// message. The input slice and its messages are not mutated.
func InjectJSONInstructionIntoMessages(messages []types.Message, schema map[string]interface{}, opts InjectJSONInstructionOptions) []types.Message {
	hasLeadingSystem := len(messages) > 0 && messages[0].Role == types.RoleSystem
	existingText := ""
	if hasLeadingSystem {
		existingText = concatTextContent(messages[0].Content)
	}

	systemMessage := types.Message{
		Role:    types.RoleSystem,
		Content: []types.ContentPart{types.TextContent{Text: InjectJSONInstruction(existingText, schema, opts)}},
	}

	rest := messages
	if hasLeadingSystem {
		rest = messages[1:]
	}
	out := make([]types.Message, 0, len(rest)+1)
	out = append(out, systemMessage)
	out = append(out, rest...)
	return out
}

func concatTextContent(parts []types.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if tp, ok := part.(types.TextContent); ok {
			b.WriteString(tp.Text)
		}
	}
	return b.String()
}

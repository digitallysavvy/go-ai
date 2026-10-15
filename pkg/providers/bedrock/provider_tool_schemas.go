package bedrock

import (
	_ "embed"
	"encoding/json"
	"sync"
)

// providerToolSchemasJSON holds, per Anthropic provider tool id, the JSON
// schema the TS SDK derives from the tool factory's zod inputSchema
// (zod toJSONSchema, draft-7, io "input", plus additionalProperties:false on
// every object). TS amazon-bedrock-prepare-tools.ts sends exactly this as the
// toolSpec inputSchema of an Anthropic provider tool; the Go tool's own
// Parameters (property descriptions, no $schema/additionalProperties) are not
// what TS sends. Generated from packages/anthropic/src/tool/*.ts at
// b658e8503c; key order is the zod declaration order and is kept on the wire.
//
//go:embed provider_tool_schemas.json
var providerToolSchemasJSON []byte

var (
	providerToolSchemasOnce sync.Once
	providerToolSchemas     map[string]json.RawMessage
)

// anthropicProviderToolSchema returns the TS zod-derived input schema for an
// Anthropic provider tool id (e.g. "anthropic.bash_20250124").
func anthropicProviderToolSchema(id string) (json.RawMessage, bool) {
	providerToolSchemasOnce.Do(func() {
		_ = json.Unmarshal(providerToolSchemasJSON, &providerToolSchemas) //nolint:errcheck // embedded, validated by test
	})
	s, ok := providerToolSchemas[id]
	return s, ok
}

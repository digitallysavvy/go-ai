package tool

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// providerArgs renders a typed tool config as the TS args object of the
// matching provider tool factory: the camelCase fields the caller set (the
// config structs' JSON tags are the TS arg names). Provider-defined tools
// carry it in ProviderArgs, which the AI Gateway sends as the provider
// tool's `args` (TS `{type: "provider", id, name, args}`).
func providerArgs(config interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	b, err := json.Marshal(config)
	if err != nil {
		return out
	}
	if err := json.Unmarshal(b, &out); err != nil || out == nil {
		return map[string]interface{}{}
	}
	return out
}

// schemaFromJSON decodes a JSON Schema literal. Each call returns a fresh
// map, so callers may modify a tool's schema without affecting other tools.
func schemaFromJSON(s string) map[string]interface{} {
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		panic("openai tool: invalid schema literal: " + err.Error())
	}
	return out
}

// providerTool fills the provider-defined tool identity TS createProvider*
// ToolFactory sets: type "provider", the TS id and the TS args.
func providerTool(t types.Tool, id string, args map[string]interface{}) types.Tool {
	t.Type = types.ToolTypeProviderDefined
	t.ProviderID = id
	if args == nil {
		args = map[string]interface{}{}
	}
	t.ProviderArgs = args
	return t
}

// webSearchArgs is providerArgs for the web search configs, plus the
// userLocation type TS requires (`type: 'approximate'`), which the request
// builder also defaults.
func webSearchArgs(config interface{}) map[string]interface{} {
	args := providerArgs(config)
	if loc, ok := args["userLocation"].(map[string]interface{}); ok {
		if _, set := loc["type"]; !set {
			loc["type"] = "approximate"
		}
	}
	return args
}

// The input and output schemas below are the TS tool schemas (packages/openai/src/tool/*.ts at e64ed82f27) as zod
// toJSONSchema renders them (target draft-7, io "input", reused "inline").
// Provider tool calls are validated against the input schema and
// ValidateUIMessages checks tool outputs against the output schema.
const (
	emptyObjectSchemaJSON = `{"type":"object","properties":{}}`

	webSearchOutputSchemaJSON = `{"type":"object","properties":{"action":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"search"},"query":{"type":"string"},"queries":{"type":"array","items":{"type":"string"}}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"openPage"},"url":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"findInPage"},"url":{"anyOf":[{"type":"string"},{"type":"null"}]},"pattern":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["type"]}]},"sources":{"type":"array","items":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"url"},"url":{"type":"string"}},"required":["type","url"]},{"type":"object","properties":{"type":{"type":"string","const":"api"},"name":{"type":"string"}},"required":["type","name"]}]}}}}`

	webSearchPreviewOutputSchemaJSON = `{"type":"object","properties":{"action":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"search"},"query":{"type":"string"}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"openPage"},"url":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"findInPage"},"url":{"anyOf":[{"type":"string"},{"type":"null"}]},"pattern":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["type"]}]}}}`

	codeInterpreterInputSchemaJSON = `{"type":"object","properties":{"code":{"anyOf":[{"type":"string"},{"type":"null"}]},"containerId":{"type":"string"}},"required":["containerId"]}`

	codeInterpreterOutputSchemaJSON = `{"type":"object","properties":{"outputs":{"anyOf":[{"type":"array","items":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"logs"},"logs":{"type":"string"}},"required":["type","logs"]},{"type":"object","properties":{"type":{"type":"string","const":"image"},"url":{"type":"string"}},"required":["type","url"]}]}},{"type":"null"}]}}}`

	fileSearchOutputSchemaJSON = `{"type":"object","properties":{"queries":{"type":"array","items":{"type":"string"}},"results":{"anyOf":[{"type":"array","items":{"type":"object","properties":{"attributes":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{}},"fileId":{"type":"string"},"filename":{"type":"string"},"score":{"type":"number"},"text":{"type":"string"}},"required":["attributes","fileId","filename","score","text"]}},{"type":"null"}]}},"required":["queries","results"]}`

	imageGenerationOutputSchemaJSON = `{"type":"object","properties":{"result":{"type":"string"}},"required":["result"]}`

	mcpOutputSchemaJSON = `{"type":"object","properties":{"type":{"type":"string","const":"call"},"serverLabel":{"type":"string"},"name":{"type":"string"},"arguments":{"type":"string"},"output":{"anyOf":[{"type":"string"},{"type":"null"}]},"error":{"anyOf":[{"type":"string"},{"$ref":"#/definitions/__schema0"}]}},"required":["type","serverLabel","name","arguments"],"definitions":{"__schema0":{"anyOf":[{"type":"string"},{"type":"number"},{"type":"boolean"},{"type":"null"},{"type":"array","items":{"$ref":"#/definitions/__schema0"}},{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{"$ref":"#/definitions/__schema0"}}]}}}`

	toolSearchInputSchemaJSON = `{"type":"object","properties":{"arguments":{},"call_id":{"anyOf":[{"type":"string"},{"type":"null"}]}}}`

	toolSearchOutputSchemaJSON = `{"type":"object","properties":{"tools":{"type":"array","items":{"type":"object","propertyNames":{"type":"string"},"additionalProperties":{}}}},"required":["tools"]}`
)

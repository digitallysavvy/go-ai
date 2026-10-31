package xai

import (
	"context"
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// providerExecutedNoop returns an Execute function for provider-executed tools.
// These tools are run by xAI's servers; local execution reports provider execution.
func providerExecutedNoop(toolName string) func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
	return func(_ context.Context, _ map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
		return nil, &types.ToolExecutionError{
			ToolCallID:       opts.ToolCallID,
			ToolName:         toolName,
			Err:              context.Canceled,
			ProviderExecuted: true,
		}
	}
}

// xaiProviderTool fills the provider-defined tool identity TS
// createProviderExecutedToolFactory sets: type "provider", the TS id and the
// TS camelCase args. Name stays the TS id (prepareXAIResponsesTools
// dispatches on it) and the typed ProviderOptions config still drives the
// direct xAI request; the AI Gateway sends {type: "provider", id, name,
// args}.
func xaiProviderTool(t types.Tool, args map[string]interface{}) types.Tool {
	t.Type = types.ToolTypeProviderDefined
	t.ProviderID = t.Name
	if args == nil {
		args = map[string]interface{}{}
	}
	t.ProviderArgs = args
	return t
}

// xaiSchema decodes a JSON Schema literal into a fresh map.
func xaiSchema(s string) map[string]interface{} {
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		panic("xai: invalid schema literal: " + err.Error())
	}
	return out
}

// The tool schemas below are the TS xai tool schemas (packages/xai/src/tool/*.ts at e64ed82f27) as zod toJSONSchema
// renders them (target draft-7, io "input", reused "inline"). Provider tool
// calls are validated against the input schema and ValidateUIMessages checks
// tool outputs against the output schema.
const (
	xaiEmptyInputSchemaJSON = `{"type":"object","properties":{}}`

	xaiNoInputSchemaJSON = `{"type":"object","properties":{},"description":"no input parameters"}`

	xaiCodeExecutionOutputSchemaJSON = `{"type":"object","properties":{"output":{"type":"string","description":"the output of the code execution"},"error":{"description":"any error that occurred","type":"string"}},"required":["output"]}`

	xaiViewImageOutputSchemaJSON = `{"type":"object","properties":{"description":{"type":"string","description":"description of the image"},"objects":{"description":"objects detected in the image","type":"array","items":{"type":"string"}}},"required":["description"]}`

	xaiViewXVideoOutputSchemaJSON = `{"type":"object","properties":{"transcript":{"description":"transcript of the video","type":"string"},"description":{"type":"string","description":"description of the video content"},"duration":{"description":"duration in seconds","type":"number"}},"required":["description"]}`

	xaiWebSearchOutputSchemaJSON = `{"type":"object","properties":{"action":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"search"},"query":{"type":"string"},"queries":{"type":"array","items":{"type":"string"}}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"openPage"},"url":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["type"]},{"type":"object","properties":{"type":{"type":"string","const":"findInPage"},"url":{"anyOf":[{"type":"string"},{"type":"null"}]},"pattern":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["type"]}]},"sources":{"type":"array","items":{"type":"object","properties":{"type":{"type":"string","const":"url"},"url":{"type":"string"}},"required":["type","url"]}}}}`

	xaiXSearchOutputSchemaJSON = `{"type":"object","properties":{"query":{"type":"string"},"posts":{"type":"array","items":{"type":"object","properties":{"author":{"type":"string"},"text":{"type":"string"},"url":{"type":"string"},"likes":{"type":"number"}},"required":["author","text","url","likes"]}}},"required":["query","posts"]}`

	xaiImageGenerationOutputSchemaJSON = `{"type":"object","properties":{"result":{"type":"string"},"prompt":{"type":"string"}},"required":["result"]}`
)

package providerutils

import (
	"strconv"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// HasFunctionTool reports whether tools contains a function tool (as opposed
// to a provider-defined or provider-executed tool).
func HasFunctionTool(tools []types.Tool) bool {
	for _, t := range tools {
		switch t.Type {
		case "", types.ToolTypeFunction, types.ToolTypeDynamic:
			return true
		}
	}
	return false
}

// NeedsJSONResponseTool reports whether a structured-output request has to be
// served through a JSON response tool: a JSON response format with a schema
// combined with function tools (TS #21989).
func NeedsJSONResponseTool(format *provider.ResponseFormat, tools []types.Tool) bool {
	return format != nil && format.Type == "json" && format.Schema != nil && HasFunctionTool(tools)
}

// NewJSONResponseTool builds the synthetic function tool that carries a
// structured response. Its name is "json", or "json_1", "json_2", ... when
// that collides with an application tool.
func NewJSONResponseTool(format *provider.ResponseFormat, tools []types.Tool) types.Tool {
	name := "json"
	for suffix := 1; toolNameTaken(tools, name); suffix++ {
		name = "json_" + strconv.Itoa(suffix)
	}
	description := format.Description
	if description == "" {
		description = "Respond with a JSON object."
	}
	parameters := ResponseFormatJSONSchema(format.Schema)
	if ordered, ok := format.Schema.(*schema.OrderedSchema); ok && ordered != nil {
		// Keeps the schema's source key order on the wire.
		parameters = ordered
	}
	return types.Tool{
		Type:        types.ToolTypeFunction,
		Name:        name,
		Description: description,
		Parameters:  parameters,
	}
}

func toolNameTaken(tools []types.Tool, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

// JSONResponseToolChoice maps the caller's tool choice when a JSON response
// tool is added: a specific tool choice is kept, "none" forces the response
// tool, and everything else becomes "required" so the model must answer
// through a tool call.
func JSONResponseToolChoice(choice types.ToolChoice, responseToolName string) types.ToolChoice {
	switch choice.Type {
	case types.ToolChoiceTool:
		return choice
	case types.ToolChoiceNone:
		return types.ToolChoice{Type: types.ToolChoiceTool, ToolName: responseToolName}
	default:
		return types.ToolChoice{Type: types.ToolChoiceRequired}
	}
}

// JSONResponseToolText returns the structured text carried by a JSON response
// tool call: the raw argument string when present, otherwise the JSON
// encoding of the parsed arguments ("{}" when there are none).
func JSONResponseToolText(call types.ToolCall) string {
	if call.RawArguments != "" {
		return call.RawArguments
	}
	if call.Arguments == nil {
		return "{}"
	}
	raw, err := jsonstr.Marshal(call.Arguments)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

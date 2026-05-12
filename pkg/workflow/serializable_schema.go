package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// MarshalSchema converts any JSON-serializable schema into a map.
func MarshalSchema(schema interface{}) (map[string]interface{}, error) {
	if schema == nil {
		return nil, nil
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UnmarshalSchema reconstructs schema as generic JSON value.
func UnmarshalSchema(data map[string]interface{}) (interface{}, error) {
	if data == nil {
		return nil, nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var out interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SerializableToolDef is the JSON-safe representation of a workflow tool.
// Function fields such as Execute, approval predicates, and result mappers are
// intentionally omitted because workflow state must cross process boundaries.
type SerializableToolDef struct {
	Name                    string                   `json:"name"`
	Description             string                   `json:"description,omitempty"`
	Title                   string                   `json:"title,omitempty"`
	Parameters              map[string]interface{}   `json:"parameters,omitempty"`
	InputSchema             map[string]interface{}   `json:"inputSchema,omitempty"`
	Type                    string                   `json:"type,omitempty"`
	ID                      string                   `json:"id,omitempty"`
	Args                    map[string]interface{}   `json:"args,omitempty"`
	Strict                  bool                     `json:"strict,omitempty"`
	ProviderExecuted        bool                     `json:"providerExecuted,omitempty"`
	IsProviderExecuted      bool                     `json:"isProviderExecuted,omitempty"`
	SupportsDeferredResults bool                     `json:"supportsDeferredResults,omitempty"`
	InputExamples           []types.ToolInputExample `json:"inputExamples,omitempty"`
	ProviderMetadata        map[string]interface{}   `json:"providerMetadata,omitempty"`
}

// SerializeToolSet converts tools to a JSON-safe map keyed by tool name.
func SerializeToolSet(tools []types.Tool) (map[string]SerializableToolDef, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make(map[string]SerializableToolDef, len(tools))
	for _, tool := range tools {
		params, err := MarshalSchema(tool.Parameters)
		if err != nil {
			return nil, err
		}
		def := SerializableToolDef{
			Name:                    tool.Name,
			Description:             tool.Description,
			Title:                   tool.Title,
			Parameters:              params,
			InputSchema:             params,
			Strict:                  tool.Strict,
			ProviderExecuted:        tool.ProviderExecuted,
			IsProviderExecuted:      tool.ProviderExecuted,
			SupportsDeferredResults: tool.SupportsDeferredResults,
			InputExamples:           tool.InputExamples,
			ProviderMetadata:        tool.ProviderMetadata,
		}
		if tool.ProviderExecuted {
			def.Type = "provider"
		}
		if providerArgs, err := MarshalSchema(tool.ProviderOptions); err == nil {
			def.Args = providerArgs
			if id, ok := providerArgs["id"].(string); ok {
				def.ID = id
			}
			if typ, ok := providerArgs["type"].(string); ok {
				def.Type = typ
			}
		}
		out[tool.Name] = def
	}
	return out, nil
}

// ResolveSerializableTools reconstructs non-executable tool descriptors from
// serialized definitions. Callers can attach Execute functions by name after
// deserialization.
func ResolveSerializableTools(defs map[string]SerializableToolDef) []types.Tool {
	if len(defs) == 0 {
		return nil
	}
	tools := make([]types.Tool, 0, len(defs))
	for name, def := range defs {
		toolName := def.Name
		if toolName == "" {
			toolName = name
		}
		params := def.Parameters
		if params == nil {
			params = def.InputSchema
		}
		tools = append(tools, types.Tool{
			Name:                    toolName,
			Description:             def.Description,
			Title:                   def.Title,
			Parameters:              params,
			Strict:                  def.Strict,
			ProviderExecuted:        def.ProviderExecuted || def.IsProviderExecuted || def.Type == "provider",
			SupportsDeferredResults: def.SupportsDeferredResults,
			InputExamples:           def.InputExamples,
			ProviderOptions:         def.Args,
			ProviderMetadata:        def.ProviderMetadata,
		})
	}
	return tools
}

// ValidateSerializableToolInput validates a tool input against its serialized JSON Schema.
func ValidateSerializableToolInput(def SerializableToolDef, input interface{}) error {
	params := def.InputSchema
	if params == nil {
		params = def.Parameters
	}
	if params == nil {
		return nil
	}
	if err := schema.NewSimpleJSONSchema(params).Validator().Validate(input); err != nil {
		name := def.Name
		if name == "" {
			name = "tool"
		}
		return fmt.Errorf("workflow: invalid input for %s: %w", name, err)
	}
	return nil
}

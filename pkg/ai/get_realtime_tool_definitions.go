package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

type RealtimeToolDefinitionsOptions struct {
	Tools        []types.Tool
	ToolsContext map[string]interface{}
}

func GetRealtimeToolDefinitions(tools []types.Tool) ([]provider.RealtimeToolDefinition, error) {
	return GetRealtimeToolDefinitionsWithOptions(RealtimeToolDefinitionsOptions{Tools: tools})
}

func GetRealtimeToolDefinitionsWithOptions(opts RealtimeToolDefinitionsOptions) ([]provider.RealtimeToolDefinition, error) {
	definitions := make([]provider.RealtimeToolDefinition, 0, len(opts.Tools))
	for _, tool := range opts.Tools {
		switch tool.Type {
		case "", types.ToolTypeFunction, types.ToolTypeDynamic:
			var descriptionPtr *string
			if tool.DescriptionFunc != nil {
				description := tool.DescriptionFunc(context.Background(), types.ToolDescriptionOptions{Context: opts.ToolsContext[tool.Name]})
				descriptionPtr = &description
			} else if tool.Description != "" {
				description := tool.Description
				descriptionPtr = &description
			}
			parameters, err := realtimeToolParameters(tool.Parameters)
			if err != nil {
				return nil, fmt.Errorf("serialize realtime tool %q schema: %w", tool.Name, err)
			}
			def := provider.RealtimeToolDefinition{
				Type:        "function",
				Name:        tool.Name,
				Description: descriptionPtr,
				Parameters:  parameters,
			}
			definitions = append(definitions, def)
		case types.ToolTypeProviderDefined:
			continue
		default:
			return nil, fmt.Errorf("Unsupported tool type: %s", tool.Type)
		}
	}
	return definitions, nil
}

func realtimeToolParameters(parameters interface{}) (map[string]interface{}, error) {
	if parameters == nil {
		return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}, nil
	}
	if schemaProvider, ok := parameters.(interface{ JSONSchema() map[string]interface{} }); ok {
		return cloneStringAnyMap(schemaProvider.JSONSchema()), nil
	}
	if schemaValue, ok := parameters.(schema.Schema); ok {
		return cloneStringAnyMap(schemaValue.Validator().JSONSchema()), nil
	}
	raw, err := json.Marshal(parameters)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]interface{}{}
	}
	return out, nil
}

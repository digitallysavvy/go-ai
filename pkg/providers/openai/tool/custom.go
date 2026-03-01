// Package tool provides OpenAI-specific tool types for the Responses API.
//
// This package defines the CustomTool type which allows constraining model output
// with grammar or text format specifications. Custom tools are executed by the
// OpenAI Responses API, not locally.
//
// Example usage:
//
//	import openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
//
//	syntax := "lark"
//	definition := `start: OBJECT\nOBJECT: "{" pair* "}"\n...`
//	tool := openaitool.NewCustomTool("json-extractor",
//	    openaitool.WithDescription("Extract JSON matching a schema"),
//	    openaitool.WithFormat(openaitool.CustomToolFormat{
//	        Type:       "grammar",
//	        Syntax:     &syntax,
//	        Definition: &definition,
//	    }),
//	)
//	// Convert to types.Tool for use with GenerateText:
//	sdkTool := tool.ToTool()
package tool

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// CustomToolFormat defines the output format constraints for a custom tool.
// Use Type "grammar" for structured output with regex or lark syntax.
// Use Type "text" for unconstrained text output.
type CustomToolFormat struct {
	// Type is "grammar" or "text".
	Type string `json:"type"`

	// Syntax specifies the grammar syntax: "regex" or "lark".
	// Only valid when Type is "grammar".
	Syntax *string `json:"syntax,omitempty"`

	// Definition is the grammar or regex definition string.
	// Only valid when Type is "grammar".
	Definition *string `json:"definition,omitempty"`
}

// CustomTool defines an OpenAI custom tool for the Responses API.
// Custom tools constrain model output using grammar or text format specifications.
// They are executed by the OpenAI API, not locally.
type CustomTool struct {
	// Name is the unique identifier for this custom tool.
	Name string `json:"name"`

	// Description explains what the tool does (optional).
	Description *string `json:"description,omitempty"`

	// Format specifies output format constraints (optional).
	// Omit for unconstrained text output.
	Format *CustomToolFormat `json:"format,omitempty"`
}

// CustomToolOption is a functional option for configuring a CustomTool.
type CustomToolOption func(*CustomTool)

// WithDescription sets the description for a custom tool.
func WithDescription(description string) CustomToolOption {
	return func(t *CustomTool) {
		t.Description = &description
	}
}

// WithFormat sets the output format constraints for a custom tool.
func WithFormat(format CustomToolFormat) CustomToolOption {
	return func(t *CustomTool) {
		t.Format = &format
	}
}

// NewCustomTool creates a new CustomTool with the given name and options.
//
// Example:
//
//	syntax := "lark"
//	definition := `start: OBJECT\n...`
//	tool := openaitool.NewCustomTool("json-extractor",
//	    openaitool.WithDescription("Extract JSON matching a schema"),
//	    openaitool.WithFormat(openaitool.CustomToolFormat{
//	        Type:       "grammar",
//	        Syntax:     &syntax,
//	        Definition: &definition,
//	    }),
//	)
func NewCustomTool(name string, opts ...CustomToolOption) CustomTool {
	ct := CustomTool{Name: name}
	for _, opt := range opts {
		opt(&ct)
	}
	return ct
}

// ToTool converts a CustomTool to a types.Tool for use with generate functions.
// The tool name is set to "openai.custom" so the OpenAI Responses API provider
// knows to serialize it as a custom tool definition.
//
// Example:
//
//	sdkTool := tool.ToTool()
//	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
//	    Model: model,
//	    Tools: []types.Tool{sdkTool},
//	})
func (ct CustomTool) ToTool() types.Tool {
	return types.Tool{
		Name:             "openai.custom",
		Description:      func() string { if ct.Description != nil { return *ct.Description }; return "" }(),
		ProviderExecuted: true,
		ProviderOptions:  ct,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("custom tool %q is executed by the OpenAI API, not locally", ct.Name)
		},
	}
}

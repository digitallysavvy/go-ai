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
//	tool := openaitool.NewCustomTool(
//	    openaitool.WithDescription("Extract JSON matching a schema"),
//	    openaitool.WithFormat(openaitool.CustomToolFormat{
//	        Type:       "grammar",
//	        Syntax:     &syntax,
//	        Definition: &definition,
//	    }),
//	).ToTool("json-extractor")
//	// Convert to types.Tool for use with GenerateText:
//	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
//	    Model: model,
//	    Tools: []types.Tool{tool},
//	})
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
//
// The tool name is not stored in CustomTool itself — it is supplied when calling
// ToTool("tool-name"), so the name is derived from the caller's context (e.g., the
// key in a tools map), matching the TypeScript SDK's key-based naming convention.
type CustomTool struct {
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

// NewCustomTool creates a new CustomTool with the given options.
// The tool name is not provided here — supply it when calling ToTool("name").
//
// Example:
//
//	syntax := "lark"
//	definition := `start: OBJECT\n...`
//	tool := openaitool.NewCustomTool(
//	    openaitool.WithDescription("Extract JSON matching a schema"),
//	    openaitool.WithFormat(openaitool.CustomToolFormat{
//	        Type:       "grammar",
//	        Syntax:     &syntax,
//	        Definition: &definition,
//	    }),
//	).ToTool("json-extractor")
func NewCustomTool(opts ...CustomToolOption) CustomTool {
	ct := CustomTool{}
	for _, opt := range opts {
		opt(&ct)
	}
	return ct
}

// ToTool converts a CustomTool to a types.Tool for use with generate functions.
// The name parameter sets the tool's identifier — it will be used as the "name"
// field in the OpenAI Responses API wire format.
//
// Example:
//
//	sdkTool := openaitool.NewCustomTool(openaitool.WithDescription("a tool")).ToTool("my-tool")
//	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
//	    Model: model,
//	    Tools: []types.Tool{sdkTool},
//	})
func (ct CustomTool) ToTool(name string) types.Tool {
	return types.Tool{
		Name:             name,
		Description:      func() string { if ct.Description != nil { return *ct.Description }; return "" }(),
		ProviderExecuted: true,
		ProviderOptions:  ct,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("custom tool %q is executed by the OpenAI API, not locally", name)
		},
	}
}

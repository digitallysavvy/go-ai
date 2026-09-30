package openresponses

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// CustomToolFormat constrains a custom tool's raw text output, mirroring
// TS's OpenResponsesCustomToolOptions.format: either an unconstrained "text"
// output, or a "grammar"-constrained output (Syntax/Definition apply only
// then).
type CustomToolFormat struct {
	// Type is "grammar" or "text".
	Type string

	// Syntax is "regex" or "lark". Only meaningful when Type is "grammar".
	Syntax string

	// Definition is the grammar/regex definition string. Only meaningful
	// when Type is "grammar".
	Definition string
}

// CustomToolOptions configures a caller-executed Open Responses custom
// tool, mirroring TS's OpenResponsesCustomToolOptions.
type CustomToolOptions struct {
	// Description explains what the tool does (optional).
	Description string

	// Format specifies output format constraints. Nil means unconstrained
	// text output (equivalent to {Type: "text"}).
	Format *CustomToolFormat

	// Execute runs the tool locally against the model's raw text input
	// (available at ToolCallContent.Arguments["input"]). Custom tools are
	// caller-executed, not provider-executed, mirroring TS's
	// createProviderDefinedToolFactory default (isProviderExecuted: false).
	Execute types.ToolExecutor
}

// Tools is a factory for Open Responses caller-executed tools scoped to one
// customToolID (Config.CustomToolID), mirroring TS's
// createOpenResponsesTools({customToolId}).
type Tools struct {
	customToolID string
}

// NewTools returns a Tools factory for customToolID, matching the
// `${string}.${string}` id a language model's Config.CustomToolID must also
// use for the custom tool it declares to be recognized on request/response.
func NewTools(customToolID string) *Tools {
	return &Tools{customToolID: customToolID}
}

// CustomTool creates a caller-executed OpenAI-compatible custom tool.
// Custom tools accept a raw string, optionally constrained by a grammar.
// Requires endpoint support. name is the tool's identifier (TS derives it
// from the key in the `tools` map passed to generateText/streamText; Go's
// types.Tool carries it directly).
func (t *Tools) CustomTool(name string, options CustomToolOptions) types.Tool {
	args := map[string]interface{}{}
	if options.Description != "" {
		args["description"] = options.Description
	}
	if options.Format != nil {
		format := map[string]interface{}{"type": options.Format.Type}
		if options.Format.Type == "grammar" {
			format["syntax"] = options.Format.Syntax
			format["definition"] = options.Format.Definition
		}
		args["format"] = format
	}
	return types.Tool{
		Name:             name,
		Description:      options.Description,
		Type:             types.ToolTypeProviderDefined,
		ProviderID:       t.customToolID,
		ProviderArgs:     args,
		ProviderExecuted: false,
		Execute:          options.Execute,
	}
}

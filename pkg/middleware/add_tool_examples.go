package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// AddToolInputExamplesOptions configures the tool input examples middleware
type AddToolInputExamplesOptions struct {
	// Prefix is the text to prepend before examples
	// Default: "Input Examples:"
	Prefix string

	// Format is a custom formatter for each example
	// If nil, uses JSON.stringify on the example input
	// Parameters: (example, index) -> formatted string
	Format func(example types.ToolInputExample, index int) string

	// Remove indicates whether to remove the InputExamples property
	// after adding them to the description
	// Default: true
	Remove bool
}

// defaultFormatExample formats an example as JSON
func defaultFormatExample(example types.ToolInputExample, index int) string {
	data, err := json.Marshal(example.Input)
	if err != nil {
		return fmt.Sprintf("%v", example.Input)
	}
	return string(data)
}

// AddToolInputExamplesMiddleware returns middleware that appends input examples
// to tool descriptions.
//
// This is useful for providers that don't natively support the inputExamples
// property. The middleware serializes examples into the tool's description text.
//
// Example:
//
//	middleware := AddToolInputExamplesMiddleware(&AddToolInputExamplesOptions{
//		Prefix: "Input Examples:",
//		Remove: true,
//	})
//	wrapped := WrapLanguageModel(model, []*LanguageModelMiddleware{middleware}, nil, nil)
func AddToolInputExamplesMiddleware(options *AddToolInputExamplesOptions) *LanguageModelMiddleware {
	if options == nil {
		options = &AddToolInputExamplesOptions{
			Prefix: "Input Examples:",
			Format: defaultFormatExample,
			Remove: true,
		}
	}

	if options.Prefix == "" {
		options.Prefix = "Input Examples:"
	}

	if options.Format == nil {
		options.Format = defaultFormatExample
	}

	return &LanguageModelMiddleware{
		SpecificationVersion: "v3",

		TransformParams: func(
			ctx context.Context,
			callType string,
			params *provider.GenerateOptions,
			model provider.LanguageModel,
		) (*provider.GenerateOptions, error) {
			// If no tools, return params unchanged
			if len(params.Tools) == 0 {
				return params, nil
			}

			// Transform tools to include examples in descriptions
			transformedTools := make([]types.Tool, len(params.Tools))
			for i, tool := range params.Tools {
				transformedTools[i] = tool

				// Only process tools that have input examples
				if len(tool.InputExamples) == 0 {
					continue
				}

				// Format each example
				exampleStrings := make([]string, len(tool.InputExamples))
				for j, example := range tool.InputExamples {
					exampleStrings[j] = options.Format(example, j)
				}

				// Build the examples section
				formattedExamples := strings.Join(exampleStrings, "\n")
				examplesSection := fmt.Sprintf("%s\n%s", options.Prefix, formattedExamples)

				// Append to description or create new description
				var toolDescription string
				if tool.Description != "" {
					toolDescription = fmt.Sprintf("%s\n\n%s", tool.Description, examplesSection)
				} else {
					toolDescription = examplesSection
				}

				transformedTools[i].Description = toolDescription

				// Remove InputExamples if requested
				if options.Remove {
					transformedTools[i].InputExamples = nil
				}
			}

			// Return new params with transformed tools
			newParams := *params
			newParams.Tools = transformedTools
			return &newParams, nil
		},
	}
}

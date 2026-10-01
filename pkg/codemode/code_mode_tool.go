package codemode

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// DefaultToolName is the Tool.Name assigned by CreateCodeModeTool and
// CodeModeTool. Go tools are identified by their Name field (see
// ai.PrepareToolsForToolCallers), unlike TypeScript where a tool's name is
// the key under which it is registered in the tools record; callers that
// want a different name can copy the returned types.Tool and overwrite
// Name (its Execute/ExperimentalToolCaller behavior does not depend on it).
const DefaultToolName = "codeMode"

// CreateCodeModeTool creates an AI SDK tool that executes code-mode
// JavaScript in an isolated sandbox, with tools reachable inside the
// sandbox as `tools.<name>(input)`. Mirrors TypeScript's
// experimental_createCodeModeTool.
//
// tools is a Go map (a Go ToolSet), which has no defined iteration order,
// unlike TypeScript's tools object (whose keys iterate in insertion
// order); the generated catalog/prompt therefore lists tools in sorted
// name order here, not caller declaration order (see orderedFromToolSet).
// Route tools through CodeModeTool + ai.ExperimentalToolCallers instead
// when declaration-order catalog output matters: its
// ToolCallerDefinition.Bind receives tools as an already-ordered
// []types.Tool from ai.PrepareToolsForToolCallers and reproduces that
// order exactly.
func CreateCodeModeTool(tools ToolSet, options Options) types.Tool {
	return createCodeModeToolWithDiscovery(orderedFromToolSet(tools), options, ToolDiscoveryDescription)
}

// createCodeModeToolWithDiscovery builds the codeMode tool from tools
// already in the exact order the catalog/prompt should render them.
func createCodeModeToolWithDiscovery(tools []types.Tool, options Options, discovery ToolDiscovery) types.Tool {
	opts := options
	toolSet := make(ToolSet, len(tools))
	for _, t := range tools {
		toolSet[t.Name] = t
	}
	return types.Tool{
		Name:        DefaultToolName,
		Description: buildCodeModeToolDescriptionOrdered(tools, discovery),
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"js": map[string]interface{}{
					"type":        "string",
					"description": "Code-mode TypeScript source to execute. The code-mode context lists the available global `tools` API, input types, and call examples.",
				},
			},
			"required":             []interface{}{"js"},
			"additionalProperties": false,
		},
		Execute: func(ctx context.Context, input map[string]interface{}, execOptions types.ToolExecutionOptions) (interface{}, error) {
			js, _ := input["js"].(string)
			return RunCodeMode(ctx, RunInput{
				JS:                   js,
				Tools:                toolSet,
				ToolExecutionOptions: &execOptions,
				Options:              &opts,
			})
		},
	}
}

// CodeModeTool creates a code-mode tool caller for use with
// ai.ExperimentalToolCallers: list DefaultToolName (or a custom Name you
// assign to the returned Tool) among a host tool's ToolCallers entries to
// route calls to it through code mode instead of (or in addition to)
// direct model calls. Mirrors TypeScript's experimental_codeModeTool.
//
// The tool catalog/prompt this caller generates once bound lists tools in
// the same order ai.PrepareToolsForToolCallers determined (ultimately the
// declaration order of the tools passed to generation), matching
// TypeScript's insertion-order behavior -- see ToolCallerDefinition.Bind's
// doc.
func CodeModeTool(options ToolCallerOptions) types.Tool {
	discovery := options.ToolDiscovery
	if discovery == "" {
		discovery = ToolDiscoveryDescription
	}
	codeModeOptions := options.Options

	tool := createCodeModeToolWithDiscovery(nil, codeModeOptions, discovery)
	tool.ExperimentalToolCaller = &types.ToolCallerDefinition{
		Type: types.ToolCallerTypeLocal,
		Bind: func(boundTools []types.Tool) types.Tool {
			return createCodeModeToolWithDiscovery(boundTools, codeModeOptions, discovery)
		},
	}
	if discovery == ToolDiscoveryConversation {
		tool.ExperimentalToolCaller.PrepareModelMessage = func(boundTools []types.Tool) *string {
			msg := buildCodeModeToolCatalogMessageOrdered(boundTools)
			return &msg
		}
	}
	return tool
}

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
func CreateCodeModeTool(tools ToolSet, options Options) types.Tool {
	return createCodeModeToolWithDiscovery(tools, options, ToolDiscoveryDescription)
}

func createCodeModeToolWithDiscovery(tools ToolSet, options Options, discovery ToolDiscovery) types.Tool {
	opts := options
	return types.Tool{
		Name:        DefaultToolName,
		Description: BuildCodeModeToolDescription(tools, discovery),
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
				Tools:                tools,
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
func CodeModeTool(options ToolCallerOptions) types.Tool {
	discovery := options.ToolDiscovery
	if discovery == "" {
		discovery = ToolDiscoveryDescription
	}
	codeModeOptions := options.Options

	tool := createCodeModeToolWithDiscovery(ToolSet{}, codeModeOptions, discovery)
	tool.ExperimentalToolCaller = &types.ToolCallerDefinition{
		Type: types.ToolCallerTypeLocal,
		Bind: func(boundTools map[string]types.Tool) types.Tool {
			return createCodeModeToolWithDiscovery(boundTools, codeModeOptions, discovery)
		},
	}
	if discovery == ToolDiscoveryConversation {
		tool.ExperimentalToolCaller.PrepareModelMessage = func(boundTools map[string]types.Tool) *string {
			msg := BuildCodeModeToolCatalogMessage(boundTools)
			return &msg
		}
	}
	return tool
}

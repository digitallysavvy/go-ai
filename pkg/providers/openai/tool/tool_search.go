package tool

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ToolSearchArgs configures the behavior of a ToolSearch provider tool.
type ToolSearchArgs struct {
	// Execution is "server" (default) or "client".
	//
	// Server mode: OpenAI performs the search across deferred tools internally.
	// No tool_search_call event is emitted; the tool config is sent in the request.
	//
	// Client mode: the model emits a tool_search_call event. The client's Execute
	// function is called with the search arguments and returns matching tools.
	Execution string

	// Description describes the search capability. Primarily used for client mode.
	Description string

	// Parameters is the JSON schema for search arguments.
	// Primarily used for client execution mode.
	Parameters map[string]interface{}

	// Execute is invoked when the model emits a tool_search_call in client mode.
	// The input map contains the parsed JSON arguments from the model.
	// It should return a value (e.g., a list of tool names) or an error.
	// If nil, a default error-returning function is used.
	Execute func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error)
}

// ToolSearchOptions holds the serializable configuration for a ToolSearch tool.
// Stored in types.Tool.ProviderOptions so that PrepareTools can produce the
// correct ToolSearchToolDef wire format.
type ToolSearchOptions struct {
	// Execution is "server" or "client".
	Execution string
}

// ToolSearch creates a provider tool for the OpenAI Responses API tool_search feature.
//
// Server mode (default): OpenAI resolves deferred tool matches internally.
// The tool definition is sent in the request; no tool_search_call event is emitted.
//
// Client mode: The model emits a tool_search_call event which is routed to the
// Execute function. The client is responsible for returning matching tools.
//
// Example — server mode:
//
//	searchTool := openaitool.ToolSearch(openaitool.ToolSearchArgs{})
//
// Example — client mode:
//
//	searchTool := openaitool.ToolSearch(openaitool.ToolSearchArgs{
//	    Execution:   "client",
//	    Description: "Find tools matching a query",
//	    Parameters: map[string]interface{}{
//	        "type": "object",
//	        "properties": map[string]interface{}{
//	            "query": map[string]interface{}{"type": "string"},
//	        },
//	    },
//	    Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
//	        query, _ := input["query"].(string)
//	        return []string{"get_weather", "search_web"}, nil
//	    },
//	})
func ToolSearch(args ToolSearchArgs) types.Tool {
	execution := args.Execution
	if execution == "" {
		execution = "server"
	}

	execute := args.Execute
	if execute == nil {
		execute = func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("openai tool_search (%s mode): no Execute function provided", execution)
		}
	}

	return types.Tool{
		Name:             "openai.tool_search",
		Description:      args.Description,
		Parameters:       args.Parameters,
		ProviderExecuted: execution == "server",
		ProviderOptions: ToolSearchOptions{
			Execution: execution,
		},
		Execute: execute,
	}
}

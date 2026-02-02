package xai

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// FileSearchConfig contains configuration for the xAI FileSearch tool
type FileSearchConfig struct {
	// VectorStoreIDs is a list of vector store IDs to search
	// The file search tool will search across all specified vector stores
	VectorStoreIDs []string

	// MaxNumResults is the maximum number of results to return
	// Defaults to 10 if not specified
	MaxNumResults int
}

// FileSearch creates a provider-executed tool for searching vector stores
// This tool is executed by xAI's servers and enables RAG (Retrieval Augmented Generation) applications
//
// The tool returns search results including:
// - File IDs and filenames
// - Relevance scores
// - Matching text snippets
//
// Example:
//
//	tool := xai.FileSearch(xai.FileSearchConfig{
//	    VectorStoreIDs: []string{"vs_123", "vs_456"},
//	    MaxNumResults:  5,
//	})
func FileSearch(config FileSearchConfig) types.Tool {
	// Set default max results if not specified
	maxResults := config.MaxNumResults
	if maxResults == 0 {
		maxResults = 10
	}

	// Build the parameters schema
	properties := map[string]interface{}{
		"query": map[string]interface{}{
			"type":        "string",
			"description": "Search query to find relevant information in vector stores",
		},
	}

	// Only add optional parameters if they were specified
	if len(config.VectorStoreIDs) > 0 {
		properties["vectorStoreIds"] = map[string]interface{}{
			"type":        "array",
			"description": "Vector store IDs to search",
			"items": map[string]interface{}{
				"type": "string",
			},
			"default": config.VectorStoreIDs,
		}
	}

	if config.MaxNumResults > 0 {
		properties["maxNumResults"] = map[string]interface{}{
			"type":        "integer",
			"description": "Maximum number of results to return",
			"default":     maxResults,
		}
	}

	parameters := map[string]interface{}{
		"type":       "object",
		"properties": properties,
		"required":   []string{"query"},
	}

	return types.Tool{
		Name:        "xai.file_search",
		Description: "Search vector stores for relevant information. Returns queries and results including file IDs, filenames, relevance scores, and matching text snippets.",
		Title:       "File Search",
		Parameters:  parameters,
		// This tool is executed by xAI's servers, not locally
		ProviderExecuted: true,
		// Execute function is not needed for provider-executed tools
		// The provider will handle execution and return results
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			// This should never be called for provider-executed tools
			// The provider handles execution
			return nil, &types.ToolExecutionError{
				ToolCallID:       options.ToolCallID,
				ToolName:         "xai.file_search",
				Err:              context.Canceled,
				ProviderExecuted: true,
			}
		},
	}
}

// FileSearchWithDefaults creates a FileSearch tool with default configuration
// Uses default max results of 10
func FileSearchWithDefaults(vectorStoreIDs []string) types.Tool {
	return FileSearch(FileSearchConfig{
		VectorStoreIDs: vectorStoreIDs,
		MaxNumResults:  10,
	})
}

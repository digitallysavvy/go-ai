package xai

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

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
//
// FileSearchOptions holds the config used by prepareXAIResponsesTools to serialize
// the file_search tool into the Responses API request body.
type FileSearchOptions struct {
	VectorStoreIDs []string
	MaxNumResults  int
}

func FileSearch(config FileSearchConfig) types.Tool {
	// TS xai fileSearch: inputSchema z.object({}). The vector store IDs and
	// result limit are tool args (ProviderOptions), not model input.
	parameters := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}

	// TS fileSearch({vectorStoreIds: undefined}) has no vectorStoreIds arg
	// (JSON drops undefined); an empty list is kept.
	args := map[string]interface{}{}
	if config.VectorStoreIDs != nil {
		args["vectorStoreIds"] = config.VectorStoreIDs
	}
	if config.MaxNumResults > 0 {
		args["maxNumResults"] = config.MaxNumResults
	}
	return xaiProviderTool(types.Tool{
		Name:        "xai.file_search",
		Description: "Search vector stores for relevant information. Returns queries and results including file IDs, filenames, relevance scores, and matching text snippets.",
		Title:       "File Search",
		Parameters:  parameters,
		// TS outputSchema: {queries: string[], results: {fileId, filename,
		// score (0-1), text}[] | null} (ValidateUIMessages checks outputs).
		OutputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"queries": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"results": map[string]interface{}{
					"anyOf": []interface{}{
						map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"fileId":   map[string]interface{}{"type": "string"},
									"filename": map[string]interface{}{"type": "string"},
									"score":    map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
									"text":     map[string]interface{}{"type": "string"},
								},
								"required": []interface{}{"fileId", "filename", "score", "text"},
							},
						},
						map[string]interface{}{"type": "null"},
					},
				},
			},
			"required": []interface{}{"queries", "results"},
		},
		// ProviderOptions carries the typed config for API serialization by prepareXAIResponsesTools.
		// max_num_results is sent only when set (TS sends it only if given).
		ProviderOptions: FileSearchOptions(config),
		// This tool is executed by xAI's servers, not locally
		ProviderExecuted: true,
		Execute:          providerExecutedNoop("xai.file_search"),
	}, args)
}

// FileSearchWithDefaults creates a FileSearch tool with default configuration
// Uses default max results of 10
func FileSearchWithDefaults(vectorStoreIDs []string) types.Tool {
	return FileSearch(FileSearchConfig{
		VectorStoreIDs: vectorStoreIDs,
		MaxNumResults:  10,
	})
}

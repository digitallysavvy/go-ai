package tool

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// CodeInterpreterContainer configures an auto-created code interpreter container.
type CodeInterpreterContainer struct {
	FileIDs []string `json:"fileIds,omitempty"`
}

// CodeInterpreterConfig configures the provider-executed code_interpreter tool.
// Container may be a string container ID or CodeInterpreterContainer.
type CodeInterpreterConfig struct {
	Container interface{} `json:"container,omitempty"`
}

// FileSearchRanking configures ranking options for the file_search tool.
type FileSearchRanking struct {
	Ranker         string   `json:"ranker,omitempty"`
	ScoreThreshold *float64 `json:"scoreThreshold,omitempty"`
}

// FileSearchConfig configures the provider-executed file_search tool.
type FileSearchConfig struct {
	VectorStoreIDs []string           `json:"vectorStoreIds"`
	MaxNumResults  *int               `json:"maxNumResults,omitempty"`
	Ranking        *FileSearchRanking `json:"ranking,omitempty"`
	Filters        interface{}        `json:"filters,omitempty"`
}

// ImageGenerationMask configures an optional inpainting mask for image_generation.
type ImageGenerationMask struct {
	FileID   string `json:"fileId,omitempty"`
	ImageURL string `json:"imageUrl,omitempty"`
}

// ImageGenerationConfig configures the provider-executed image_generation tool.
type ImageGenerationConfig struct {
	// Action is "generate", "edit", or "auto".
	Action            string               `json:"action,omitempty"`
	Background        string               `json:"background,omitempty"`
	InputFidelity     string               `json:"inputFidelity,omitempty"`
	InputImageMask    *ImageGenerationMask `json:"inputImageMask,omitempty"`
	Model             string               `json:"model,omitempty"`
	Moderation        string               `json:"moderation,omitempty"`
	OutputCompression *int                 `json:"outputCompression,omitempty"`
	OutputFormat      string               `json:"outputFormat,omitempty"`
	PartialImages     *int                 `json:"partialImages,omitempty"`
	Quality           string               `json:"quality,omitempty"`
	Size              string               `json:"size,omitempty"`
}

// CodeInterpreter creates an OpenAI provider-executed code_interpreter tool
// (TS openai.tools.codeInterpreter, id "openai.code_interpreter").
func CodeInterpreter(config CodeInterpreterConfig) types.Tool {
	tool := providerExecutedHostedTool("openai.code_interpreter", config, providerArgs(config))
	// TS codeInterpreterInputSchema: {code: z.string().nullish(),
	// containerId: z.string()}.
	tool.Parameters = schemaFromJSON(codeInterpreterInputSchemaJSON)
	tool.OutputSchema = schemaFromJSON(codeInterpreterOutputSchemaJSON)
	return tool
}

// FileSearch creates an OpenAI provider-executed file_search tool (TS
// openai.tools.fileSearch, id "openai.file_search").
func FileSearch(config FileSearchConfig) types.Tool {
	args := providerArgs(config)
	if config.VectorStoreIDs == nil {
		// TS fileSearch({vectorStoreIds: undefined}) has no vectorStoreIds
		// arg (JSON drops undefined); an empty list is kept.
		delete(args, "vectorStoreIds")
	}
	tool := providerExecutedHostedTool("openai.file_search", config, args)
	tool.OutputSchema = schemaFromJSON(fileSearchOutputSchemaJSON)
	return tool
}

// ImageGeneration creates an OpenAI provider-executed image_generation tool
// (TS openai.tools.imageGeneration, id "openai.image_generation").
func ImageGeneration(config ImageGenerationConfig) types.Tool {
	tool := providerExecutedHostedTool("openai.image_generation", config, providerArgs(config))
	tool.OutputSchema = schemaFromJSON(imageGenerationOutputSchemaJSON)
	return tool
}

// providerExecutedHostedTool builds a hosted tool. Name stays the TS id (the
// direct OpenAI provider dispatches on it); Type, ProviderID and
// ProviderArgs make it a provider tool on the AI Gateway too; options is the
// typed config the OpenAI request builder reads.
func providerExecutedHostedTool(name string, options interface{}, args map[string]interface{}) types.Tool {
	return providerTool(types.Tool{
		Name:             name,
		ProviderExecuted: true,
		ProviderOptions:  options,
		// TS input schema of file_search / image_generation: z.object({}).
		Parameters: schemaFromJSON(emptyObjectSchemaJSON),
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("%s is executed by the OpenAI API, not locally", name)
		},
	}, name, args)
}

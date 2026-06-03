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

// CodeInterpreter creates an OpenAI provider-executed code_interpreter tool.
func CodeInterpreter(config CodeInterpreterConfig) types.Tool {
	return providerExecutedHostedTool("openai.code_interpreter", config)
}

// FileSearch creates an OpenAI provider-executed file_search tool.
func FileSearch(config FileSearchConfig) types.Tool {
	return providerExecutedHostedTool("openai.file_search", config)
}

// ImageGeneration creates an OpenAI provider-executed image_generation tool.
func ImageGeneration(config ImageGenerationConfig) types.Tool {
	return providerExecutedHostedTool("openai.image_generation", config)
}

func providerExecutedHostedTool(name string, options interface{}) types.Tool {
	return types.Tool{
		Name:             name,
		ProviderExecuted: true,
		ProviderOptions:  options,
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("%s is executed by the OpenAI API, not locally", name)
		},
	}
}

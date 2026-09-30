package bedrock

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
)

// EmbeddingOptions configures AWS Bedrock embedding generation
type EmbeddingOptions struct {
	// Cohere-specific options (for cohere.embed-* models)
	CohereOptions *CohereEmbeddingOptions `json:"cohereOptions,omitempty"`

	// Titan-specific options (for amazon.titan-embed-* models)
	TitanOptions *TitanEmbeddingOptions `json:"titanOptions,omitempty"`

	// Nova-specific options (for amazon.nova-* embed models)
	NovaOptions *NovaEmbeddingOptions `json:"novaOptions,omitempty"`

	// ModelFamily overrides model-family detection ("titan", "cohere", or
	// "nova"). Use this when the model ID does not identify the underlying
	// model family (e.g. a custom inference profile). Mirrors TS
	// AmazonBedrockEmbeddingModelSettings.modelFamily.
	ModelFamily string `json:"modelFamily,omitempty"`
}

// EmbeddingModelFamily identifies which wire format an embedding request/response uses.
type EmbeddingModelFamily string

const (
	EmbeddingModelFamilyTitan  EmbeddingModelFamily = "titan"
	EmbeddingModelFamilyCohere EmbeddingModelFamily = "cohere"
	EmbeddingModelFamilyNova   EmbeddingModelFamily = "nova"
)

// CohereEmbeddingOptions extends options for Cohere embedding models on Bedrock
type CohereEmbeddingOptions struct {
	// OutputDimension specifies the number of dimensions for the output embedding
	// Only available for cohere.embed-v3 and newer models
	// Supported values: 256, 512, 1024, 1536
	// Default: 1536
	OutputDimension *cohere.OutputDimension `json:"outputDimension,omitempty"`

	// InputType specifies the type of input for optimization
	// Required for Cohere models on Bedrock
	// - "search_document": For embeddings stored in a vector database
	// - "search_query": For search queries against a vector database
	// - "classification": For embeddings passed through a text classifier
	// - "clustering": For embeddings used in clustering algorithms
	// Default: "search_query"
	InputType cohere.InputType `json:"inputType,omitempty"`

	// Truncate specifies how to handle inputs longer than the maximum token length
	// - "NONE": Return an error if input exceeds max length
	// - "START": Discard the start of the input
	// - "END": Discard the end of the input
	Truncate cohere.TruncateMode `json:"truncate,omitempty"`
}

// TitanEmbeddingOptions for Titan embedding models on Bedrock
type TitanEmbeddingOptions struct {
	// Dimensions specifies the number of dimensions for Titan v2 models
	// Only supported in amazon.titan-embed-text-v2:0
	// Supported values: 256, 512, 1024
	// Default: 1024
	Dimensions *int `json:"dimensions,omitempty"`

	// Normalize flag indicating whether to normalize the output embeddings
	// Only supported in amazon.titan-embed-text-v2:0
	// Default: true
	Normalize *bool `json:"normalize,omitempty"`
}

// NovaEmbeddingOptions configures Amazon Nova embedding models on Bedrock.
type NovaEmbeddingOptions struct {
	// EmbeddingDimension specifies the number of dimensions for Nova embeddings.
	// Supported values: 256, 384, 1024, 3072. Default: 1024.
	EmbeddingDimension *int `json:"embeddingDimension,omitempty"`

	// EmbeddingPurpose specifies the embedding purpose. Default: GENERIC_INDEX.
	EmbeddingPurpose string `json:"embeddingPurpose,omitempty"`

	// Truncate specifies how to handle inputs longer than the maximum token length.
	// Default: END.
	Truncate cohere.TruncateMode `json:"truncate,omitempty"`
}

// Validate validates Nova embedding options.
func (o *NovaEmbeddingOptions) Validate() error {
	if o.EmbeddingDimension != nil {
		switch *o.EmbeddingDimension {
		case 256, 384, 1024, 3072:
		default:
			return fmt.Errorf("invalid Nova embedding dimension: %d (must be 256, 384, 1024, or 3072)", *o.EmbeddingDimension)
		}
	}
	if o.EmbeddingPurpose != "" {
		switch o.EmbeddingPurpose {
		case "GENERIC_INDEX", "TEXT_RETRIEVAL", "IMAGE_RETRIEVAL", "VIDEO_RETRIEVAL", "DOCUMENT_RETRIEVAL", "AUDIO_RETRIEVAL", "GENERIC_RETRIEVAL", "CLASSIFICATION", "CLUSTERING":
		default:
			return fmt.Errorf("invalid Nova embedding purpose: %s", o.EmbeddingPurpose)
		}
	}
	if o.Truncate != "" {
		switch o.Truncate {
		case cohere.TruncateNone, cohere.TruncateStart, cohere.TruncateEnd:
		default:
			return fmt.Errorf("invalid truncate mode: %s", o.Truncate)
		}
	}
	return nil
}

// Validate validates the embedding options
func (o *CohereEmbeddingOptions) Validate() error {
	if o.OutputDimension != nil {
		switch *o.OutputDimension {
		case cohere.Dimension256, cohere.Dimension512, cohere.Dimension1024, cohere.Dimension1536:
			// Valid dimension
		default:
			return fmt.Errorf("invalid output dimension: %d (must be 256, 512, 1024, or 1536)", *o.OutputDimension)
		}
	}

	if o.InputType != "" {
		switch o.InputType {
		case cohere.InputTypeSearchDocument, cohere.InputTypeSearchQuery, cohere.InputTypeClassification, cohere.InputTypeClustering:
			// Valid input type
		default:
			return fmt.Errorf("invalid input type: %s", o.InputType)
		}
	}

	if o.Truncate != "" {
		switch o.Truncate {
		case cohere.TruncateNone, cohere.TruncateStart, cohere.TruncateEnd:
			// Valid truncate mode
		default:
			return fmt.Errorf("invalid truncate mode: %s", o.Truncate)
		}
	}

	return nil
}

// DefaultCohereEmbeddingOptions returns default Cohere embedding options for Bedrock
func DefaultCohereEmbeddingOptions() CohereEmbeddingOptions {
	return CohereEmbeddingOptions{
		InputType: cohere.InputTypeSearchQuery,
	}
}

package bedrock

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
)

// EmbeddingOptions configures AWS Bedrock embedding generation
type EmbeddingOptions struct {
	// Cohere-specific options (for cohere.embed-* models)
	CohereOptions *CohereEmbeddingOptions

	// Titan-specific options (for amazon.titan-embed-* models)
	TitanOptions *TitanEmbeddingOptions
}

// CohereEmbeddingOptions extends options for Cohere embedding models on Bedrock
type CohereEmbeddingOptions struct {
	// OutputDimension specifies the number of dimensions for the output embedding
	// Only available for cohere.embed-v3 and newer models
	// Supported values: 256, 512, 1024, 1536
	// Default: 1536
	OutputDimension *cohere.OutputDimension

	// InputType specifies the type of input for optimization
	// Required for Cohere models on Bedrock
	// - "search_document": For embeddings stored in a vector database
	// - "search_query": For search queries against a vector database
	// - "classification": For embeddings passed through a text classifier
	// - "clustering": For embeddings used in clustering algorithms
	// Default: "search_query"
	InputType cohere.InputType

	// Truncate specifies how to handle inputs longer than the maximum token length
	// - "NONE": Return an error if input exceeds max length
	// - "START": Discard the start of the input
	// - "END": Discard the end of the input
	Truncate cohere.TruncateMode
}

// TitanEmbeddingOptions for Titan embedding models on Bedrock
type TitanEmbeddingOptions struct {
	// Dimensions specifies the number of dimensions for Titan v2 models
	// Only supported in amazon.titan-embed-text-v2:0
	// Supported values: 256, 512, 1024
	// Default: 1024
	Dimensions *int

	// Normalize flag indicating whether to normalize the output embeddings
	// Only supported in amazon.titan-embed-text-v2:0
	// Default: true
	Normalize *bool
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

package cohere

import "fmt"

// OutputDimension represents supported embedding dimensions for Cohere v3 models
type OutputDimension int

const (
	Dimension256  OutputDimension = 256
	Dimension512  OutputDimension = 512
	Dimension1024 OutputDimension = 1024
	Dimension1536 OutputDimension = 1536
)

// InputType represents the type of input for embedding optimization
type InputType string

const (
	InputTypeSearchDocument  InputType = "search_document"
	InputTypeSearchQuery     InputType = "search_query"
	InputTypeClassification  InputType = "classification"
	InputTypeClustering      InputType = "clustering"
)

// TruncateMode represents how to handle inputs that exceed max token length
type TruncateMode string

const (
	TruncateNone  TruncateMode = "NONE"
	TruncateStart TruncateMode = "START"
	TruncateEnd   TruncateMode = "END"
)

// EmbeddingOptions configures Cohere embedding generation
type EmbeddingOptions struct {
	// OutputDimension specifies the number of dimensions for the output embedding.
	// Only available for v3.0 and newer models (embed-english-v3.0, embed-multilingual-v3.0, etc.)
	// Supported values: 256, 512, 1024, 1536
	// Default varies by model (typically 1024)
	OutputDimension *OutputDimension

	// InputType specifies the type of input for optimization
	// - "search_document": For embeddings stored in a vector database
	// - "search_query": For search queries against a vector database
	// - "classification": For embeddings passed through a text classifier
	// - "clustering": For embeddings used in clustering algorithms
	// Default: "search_query"
	InputType InputType

	// Truncate specifies how to handle inputs longer than the maximum token length
	// - "NONE": Return an error if input exceeds max length
	// - "START": Discard the start of the input
	// - "END": Discard the end of the input
	// Default: "END"
	Truncate TruncateMode
}

// Validate validates the embedding options
func (o *EmbeddingOptions) Validate() error {
	if o.OutputDimension != nil {
		switch *o.OutputDimension {
		case Dimension256, Dimension512, Dimension1024, Dimension1536:
			// Valid dimension
		default:
			return fmt.Errorf("invalid output dimension: %d (must be 256, 512, 1024, or 1536)", *o.OutputDimension)
		}
	}

	if o.InputType != "" {
		switch o.InputType {
		case InputTypeSearchDocument, InputTypeSearchQuery, InputTypeClassification, InputTypeClustering:
			// Valid input type
		default:
			return fmt.Errorf("invalid input type: %s (must be search_document, search_query, classification, or clustering)", o.InputType)
		}
	}

	if o.Truncate != "" {
		switch o.Truncate {
		case TruncateNone, TruncateStart, TruncateEnd:
			// Valid truncate mode
		default:
			return fmt.Errorf("invalid truncate mode: %s (must be NONE, START, or END)", o.Truncate)
		}
	}

	return nil
}

// DefaultEmbeddingOptions returns default embedding options
func DefaultEmbeddingOptions() EmbeddingOptions {
	return EmbeddingOptions{
		InputType: InputTypeSearchDocument,
		Truncate:  "",
	}
}

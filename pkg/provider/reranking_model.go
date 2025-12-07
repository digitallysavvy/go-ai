package provider

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// RerankingModel represents a reranking model for document reranking
type RerankingModel interface {
	// Metadata
	SpecificationVersion() string
	Provider() string
	ModelID() string

	// Reranking
	DoRerank(ctx context.Context, opts *RerankOptions) (*types.RerankResult, error)
}

// RerankOptions contains options for reranking documents
type RerankOptions struct {
	// Documents to rerank (either strings or structured objects)
	Documents interface{} // []string or []map[string]interface{}

	// Query to rerank documents against
	Query string

	// TopN specifies the number of top documents to return
	// If nil or 0, all documents are returned
	TopN *int

	// Custom headers for the request
	Headers map[string]string
}

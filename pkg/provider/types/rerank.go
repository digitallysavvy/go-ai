package types

import "time"

// RerankResult contains the result of a reranking operation
type RerankResult struct {
	// Ranking contains the reranked documents with scores
	Ranking []RerankItem

	// Response metadata
	Response RerankResponse

	// Provider-specific metadata
	ProviderMetadata interface{}
}

// RerankItem represents a single reranked document
type RerankItem struct {
	// Index of the document in the original list
	Index int

	// Relevance score (higher is more relevant)
	RelevanceScore float64
}

// RerankResponse contains metadata about the reranking response
type RerankResponse struct {
	// ID of the request (if provided by the model)
	ID string

	// Timestamp of the response
	Timestamp time.Time

	// Model ID used
	ModelID string

	// Response headers
	Headers map[string][]string

	// Raw response body
	Body interface{}
}

package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// RerankOptions contains options for document reranking
type RerankOptions struct {
	// Model to use for reranking
	Model provider.RerankingModel

	// Documents to rerank (can be []string or []map[string]interface{})
	Documents interface{}

	// Query to rerank documents against
	Query string

	// TopN specifies the number of top documents to return
	// If nil or 0, all documents are returned
	TopN *int

	// Callback called when reranking finishes
	OnFinish func(result *RerankResult)
}

// RerankResult contains the result of a reranking operation
type RerankResult struct {
	// Original documents in their original order
	OriginalDocuments interface{}

	// Ranking contains indices and scores in relevance order
	Ranking []RerankItem

	// Reranked documents in relevance order
	RerankedDocuments interface{}

	// Response metadata
	Response types.RerankResponse

	// Provider-specific metadata
	ProviderMetadata interface{}
}

// RerankItem represents a single reranked document with its score
type RerankItem struct {
	// Index of the document in the original list
	OriginalIndex int

	// Relevance score (higher is more relevant)
	Score float64

	// The actual document (if documents were provided)
	Document interface{}
}

// Rerank reranks documents according to their relevance to a query
func Rerank(ctx context.Context, opts RerankOptions) (*RerankResult, error) {
	// Validate options
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	if opts.Documents == nil {
		return nil, fmt.Errorf("documents are required")
	}
	if opts.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	// Validate documents type
	var documentsSlice []interface{}
	switch docs := opts.Documents.(type) {
	case []string:
		documentsSlice = make([]interface{}, len(docs))
		for i, d := range docs {
			documentsSlice[i] = d
		}
	case []map[string]interface{}:
		documentsSlice = make([]interface{}, len(docs))
		for i, d := range docs {
			documentsSlice[i] = d
		}
	case []interface{}:
		documentsSlice = docs
	default:
		return nil, fmt.Errorf("documents must be []string, []map[string]interface{}, or []interface{}")
	}

	if len(documentsSlice) == 0 {
		// Return empty result for empty documents
		return &RerankResult{
			OriginalDocuments: opts.Documents,
			Ranking:           []RerankItem{},
			RerankedDocuments: opts.Documents,
			Response: types.RerankResponse{
				ModelID:   opts.Model.ModelID(),
				Timestamp: timeNow(),
			},
		}, nil
	}

	// Build rerank options
	rerankOpts := &provider.RerankOptions{
		Documents: opts.Documents,
		Query:     opts.Query,
		TopN:      opts.TopN,
	}

	// Call the model
	modelResult, err := opts.Model.DoRerank(ctx, rerankOpts)
	if err != nil {
		return nil, fmt.Errorf("reranking failed: %w", err)
	}

	// Build result
	ranking := make([]RerankItem, len(modelResult.Ranking))
	rerankedDocs := make([]interface{}, len(modelResult.Ranking))

	for i, item := range modelResult.Ranking {
		ranking[i] = RerankItem{
			OriginalIndex: item.Index,
			Score:         item.RelevanceScore,
			Document:      documentsSlice[item.Index],
		}
		rerankedDocs[i] = documentsSlice[item.Index]
	}

	result := &RerankResult{
		OriginalDocuments: opts.Documents,
		Ranking:           ranking,
		RerankedDocuments: rerankedDocs,
		Response:          modelResult.Response,
		ProviderMetadata:  modelResult.ProviderMetadata,
	}

	// Call finish callback
	if opts.OnFinish != nil {
		opts.OnFinish(result)
	}

	return result, nil
}

// Helper to get current time (makes testing easier)
var timeNow = func() time.Time {
	return time.Now()
}

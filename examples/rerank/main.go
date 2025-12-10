package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"sort"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// Document represents a searchable document
type Document struct {
	ID      string
	Content string
	Score   float64 // Initial retrieval score
}

// RankedDocument represents a re-ranked document
type RankedDocument struct {
	Document
	RerankScore float64
	Relevance   string
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, _ := p.LanguageModel("gpt-4")

	ctx := context.Background()

	// Example 1: Basic reranking
	fmt.Println("=== Example 1: Basic Document Reranking ===")

	query := "How do I implement authentication in a Go web application?"

	documents := []Document{
		{ID: "doc1", Content: "Go has built-in HTTP server support with net/http package.", Score: 0.75},
		{ID: "doc2", Content: "JWT tokens are commonly used for stateless authentication in web APIs.", Score: 0.82},
		{ID: "doc3", Content: "The crypto package in Go provides cryptographic functions for secure applications.", Score: 0.68},
		{ID: "doc4", Content: "OAuth 2.0 is a popular authentication framework. Go has several OAuth libraries.", Score: 0.79},
		{ID: "doc5", Content: "Session-based authentication stores user state on the server side.", Score: 0.71},
	}

	fmt.Printf("Query: %s\n\n", query)
	fmt.Println("Initial ranking (by retrieval score):")
	for i, doc := range documents {
		fmt.Printf("%d. %s (score: %.2f)\n", i+1, doc.ID, doc.Score)
	}

	// Rerank documents
	reranked := rerankDocuments(ctx, model, query, documents)

	fmt.Println("\nAfter reranking:")
	for i, doc := range reranked {
		fmt.Printf("%d. %s (rerank: %.2f, original: %.2f) - %s\n",
			i+1, doc.ID, doc.RerankScore, doc.Score, doc.Relevance)
	}
	fmt.Println()

	// Example 2: Reranking with context
	fmt.Println("=== Example 2: Context-Aware Reranking ===")

	query2 := "best practices for error handling"
	context2 := "I'm building a production REST API in Go"

	documents2 := []Document{
		{ID: "doc1", Content: "Error handling in Go uses explicit error returns instead of exceptions.", Score: 0.85},
		{ID: "doc2", Content: "Python uses try-except blocks for error handling.", Score: 0.76},
		{ID: "doc3", Content: "HTTP status codes should reflect the actual error type in REST APIs.", Score: 0.82},
		{ID: "doc4", Content: "Logging errors with context helps debugging in production.", Score: 0.80},
		{ID: "doc5", Content: "JavaScript has async/await with try-catch for promise error handling.", Score: 0.73},
	}

	fmt.Printf("Query: %s\n", query2)
	fmt.Printf("Context: %s\n\n", context2)

	reranked2 := rerankWithContext(ctx, model, query2, context2, documents2)

	fmt.Println("Reranked with context:")
	for i, doc := range reranked2 {
		fmt.Printf("%d. %s (score: %.2f) - %s\n", i+1, doc.ID, doc.RerankScore, doc.Relevance)
	}
	fmt.Println()

	// Example 3: Multi-criteria reranking
	fmt.Println("=== Example 3: Multi-Criteria Reranking ===")

	query3 := "database connection pooling"
	criteria := []string{
		"relevance to query",
		"recency of information",
		"code examples included",
		"production readiness",
	}

	documents3 := []Document{
		{ID: "doc1", Content: "Database/sql package provides connection pooling. Max connections set via SetMaxOpenConns(). Published 2023.", Score: 0.88},
		{ID: "doc2", Content: "Connection pools improve performance by reusing connections. General concept from 2015.", Score: 0.82},
		{ID: "doc3", Content: "Example: db.SetMaxOpenConns(25); db.SetMaxIdleConns(25); db.SetConnMaxLifetime(5*time.Minute)", Score: 0.91},
		{ID: "doc4", Content: "PostgreSQL and MySQL both support connection pooling. Documentation from 2018.", Score: 0.75},
	}

	fmt.Printf("Query: %s\n", query3)
	fmt.Printf("Criteria: %v\n\n", criteria)

	reranked3 := rerankMultiCriteria(ctx, model, query3, criteria, documents3)

	fmt.Println("Multi-criteria reranking:")
	for i, doc := range reranked3 {
		fmt.Printf("%d. %s (score: %.2f)\n   %s\n", i+1, doc.ID, doc.RerankScore, doc.Relevance)
	}
	fmt.Println()

	// Example 4: Hybrid scoring (combining retrieval + rerank)
	fmt.Println("=== Example 4: Hybrid Scoring ===")

	query4 := "concurrent programming patterns"
	documents4 := []Document{
		{ID: "doc1", Content: "Goroutines enable concurrent execution in Go. Use channels for communication.", Score: 0.85},
		{ID: "doc2", Content: "Thread pools manage concurrent execution in Java with ExecutorService.", Score: 0.78},
		{ID: "doc3", Content: "The select statement in Go allows waiting on multiple channel operations.", Score: 0.82},
		{ID: "doc4", Content: "Async/await in JavaScript provides asynchronous programming without callbacks.", Score: 0.72},
	}

	reranked4 := hybridRerank(ctx, model, query4, documents4, 0.6, 0.4)

	fmt.Printf("Query: %s\n\n", query4)
	fmt.Println("Hybrid scoring (60% rerank + 40% retrieval):")
	for i, doc := range reranked4 {
		fmt.Printf("%d. %s (hybrid: %.2f, rerank: %.2f, retrieval: %.2f)\n",
			i+1, doc.ID, doc.Score, doc.RerankScore, doc.Document.Score)
	}

	fmt.Println("\n=== Reranking Benefits ===")
	benefits := []string{
		"✓ Improved relevance ranking",
		"✓ Context-aware ordering",
		"✓ Better semantic understanding",
		"✓ Reduced false positives",
		"✓ Higher quality search results",
	}
	for _, benefit := range benefits {
		fmt.Println("  " + benefit)
	}

	fmt.Println("\n=== Use Cases ===")
	useCases := []string{
		"• Semantic search refinement",
		"• RAG (Retrieval-Augmented Generation)",
		"• Question answering systems",
		"• Document recommendation",
		"• Search result optimization",
	}
	for _, useCase := range useCases {
		fmt.Println("  " + useCase)
	}
}

func rerankDocuments(ctx context.Context, model provider.LanguageModel, query string, docs []Document) []RankedDocument {
	ranked := make([]RankedDocument, 0, len(docs))

	for _, doc := range docs {
		prompt := fmt.Sprintf(`Rate the relevance of this document to the query on a scale of 0-100.

Query: %s

Document: %s

Provide only a number between 0-100.`, query, doc.Content)

		result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
			Model:  model,
			Prompt: prompt,
		})

		score := 50.0 // default
		if err == nil {
			fmt.Sscanf(result.Text, "%f", &score)
		}

		relevance := "Medium"
		if score > 75 {
			relevance = "High"
		} else if score < 40 {
			relevance = "Low"
		}

		ranked = append(ranked, RankedDocument{
			Document:    doc,
			RerankScore: score / 100.0,
			Relevance:   relevance,
		})
	}

	// Sort by rerank score
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].RerankScore > ranked[j].RerankScore
	})

	return ranked
}

func rerankWithContext(ctx context.Context, model provider.LanguageModel, query string, context string, docs []Document) []RankedDocument {
	ranked := make([]RankedDocument, 0, len(docs))

	for _, doc := range docs {
		prompt := fmt.Sprintf(`Given the user's context and query, rate document relevance (0-100).

User Context: %s
Query: %s
Document: %s

Score (0-100):`, context, query, doc.Content)

		result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
			Model:  model,
			Prompt: prompt,
		})

		score := 50.0
		if err == nil {
			fmt.Sscanf(result.Text, "%f", &score)
		}

		relevance := "Relevant to context and query"
		if score < 50 {
			relevance = "Less relevant given context"
		}

		ranked = append(ranked, RankedDocument{
			Document:    doc,
			RerankScore: score / 100.0,
			Relevance:   relevance,
		})
	}

	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].RerankScore > ranked[j].RerankScore
	})

	return ranked
}

func rerankMultiCriteria(ctx context.Context, model provider.LanguageModel, query string, criteria []string, docs []Document) []RankedDocument {
	ranked := make([]RankedDocument, 0, len(docs))

	for _, doc := range docs {
		prompt := fmt.Sprintf(`Evaluate this document against multiple criteria (0-100 for each).

Query: %s
Criteria: %v
Document: %s

Provide scores and reasoning.`, query, criteria, doc.Content)

		result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
			Model:  model,
			Prompt: prompt,
		})

		// Simplified: extract a score
		score := 50.0
		if err == nil {
			// In real implementation, parse individual criterion scores
			fmt.Sscanf(result.Text, "%f", &score)
		}

		ranked = append(ranked, RankedDocument{
			Document:    doc,
			RerankScore: score / 100.0,
			Relevance:   result.Text[:min(100, len(result.Text))],
		})
	}

	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].RerankScore > ranked[j].RerankScore
	})

	return ranked
}

func hybridRerank(ctx context.Context, model provider.LanguageModel, query string, docs []Document, rerankWeight, retrievalWeight float64) []RankedDocument {
	// First, get rerank scores
	reranked := rerankDocuments(ctx, model, query, docs)

	// Combine scores
	for i := range reranked {
		hybrid := (reranked[i].RerankScore * rerankWeight) + (reranked[i].Document.Score * retrievalWeight)
		reranked[i].Score = hybrid
	}

	// Re-sort by hybrid score
	sort.Slice(reranked, func(i, j int) bool {
		return reranked[i].Score > reranked[j].Score
	})

	return reranked
}

func min(a, b int) int {
	return int(math.Min(float64(a), float64(b)))
}

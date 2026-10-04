// Recipe: retrieve context with embeddings, then answer with it.
//
// Run: OPENAI_API_KEY=... ANTHROPIC_API_KEY=... go run ./examples/recipes/rag-embeddings
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

var documents = []string{
	"Refunds are available within 30 days of purchase with a receipt.",
	"Support is open Monday to Friday, 9am to 5pm Eastern.",
	"Premium plans include priority support and a 99.9% uptime guarantee.",
}

func main() {
	ctx := context.Background()

	embedder, err := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")}).
		EmbeddingModel(openai.ModelTextEmbedding3Small)
	if err != nil {
		log.Fatal(err)
	}

	// Embed the documents once. In a real app, store these in a vector database.
	docs, err := ai.EmbedMany(ctx, ai.EmbedManyOptions{Model: embedder, Inputs: documents})
	if err != nil {
		log.Fatal(err)
	}

	question := "How long do I have to return something?"
	q, err := ai.Embed(ctx, ai.EmbedOptions{Model: embedder, Input: question})
	if err != nil {
		log.Fatal(err)
	}

	// Rank the documents by cosine similarity to the question.
	type scored struct {
		text  string
		score float64
	}
	var ranked []scored
	for i, e := range docs.Embeddings {
		score, err := ai.CosineSimilarity(q.Embedding, e)
		if err != nil {
			log.Fatal(err)
		}
		ranked = append(ranked, scored{documents[i], score})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	var contextText strings.Builder
	for _, r := range ranked[:2] {
		contextText.WriteString("- " + r.text + "\n")
	}

	model, err := anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")}).
		LanguageModel(anthropic.ClaudeSonnet5_5)
	if err != nil {
		log.Fatal(err)
	}
	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		System: "Answer using only the context. If the context does not say, say so.",
		Prompt: fmt.Sprintf("Context:\n%s\nQuestion: %s", contextText.String(), question),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Text)
}

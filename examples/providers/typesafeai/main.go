// Example: TypeSafe AI evaluation.
//
// Evaluates a shared "state" (here, a support ticket) against a choice,
// score, and boolean question in a single call using TypeSafe AI's
// experimental EvaluationModel. Requires TYPESAFE_AI_API_KEY.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/typesafeai"
)

func main() {
	tsai := typesafeai.New(typesafeai.Config{})

	state := map[string]interface{}{
		"message": "I was charged twice for my subscription. Please refund the duplicate charge.",
	}

	questions := map[string]provider.EvaluationQuestion{
		"department": {
			Type:         "choice",
			Instructions: "Which team should handle this support ticket?",
			Criteria: map[string]interface{}{
				"billing":   "Payment, invoicing, or refund issues.",
				"technical": "Bugs, outages, or errors in the product.",
				"other":     "Anything else.",
			},
		},
		"severity": {
			Type:         "score",
			Instructions: "How severe is the issue, from least to most severe?",
			Criteria: []interface{}{
				"Cosmetic; functionality is unaffected.",
				"Functionality impaired; a workaround exists.",
				"Blocking; no workaround exists.",
			},
		},
		"requestsRefund": {
			Type:         "boolean",
			Instructions: "Is the customer explicitly requesting a refund?",
		},
	}

	result, err := ai.ExperimentalEvaluate(context.Background(), ai.EvaluateOptions{
		Model:     "jev-latest",
		Provider:  tsai,
		State:     state,
		Questions: questions,
	})
	if err != nil {
		log.Fatal(err)
	}

	for id, answer := range result.Answers {
		switch answer.Type {
		case "choice":
			fmt.Printf("%s: %s\n", id, answer.Choice)
		case "score":
			fmt.Printf("%s: %.2f\n", id, *answer.Score)
		case "boolean":
			fmt.Printf("%s: P(true)=%.2f\n", id, *answer.Probability)
		}
	}
}

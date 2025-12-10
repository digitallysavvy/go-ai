package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	p := openai.New(openai.Config{
		APIKey: apiKey,
	})

	// Use gpt-4o or gpt-4o-mini for structured outputs
	model, err := p.LanguageModel("gpt-4o")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Example 1: Structured Calendar Event ===")
	generateCalendarEvent(ctx, model)

	fmt.Println("\n=== Example 2: Structured API Response ===")
	generateAPIResponse(ctx, model)

	fmt.Println("\n=== Example 3: Structured Data Extraction ===")
	extractStructuredData(ctx, model)
}

func generateCalendarEvent(ctx context.Context, model provider.LanguageModel) {
	eventSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"title": map[string]interface{}{
				"type": "string",
			},
			"date": map[string]interface{}{
				"type":   "string",
				"format": "date",
			},
			"time": map[string]interface{}{
				"type":    "string",
				"pattern": "^([01]?[0-9]|2[0-3]):[0-5][0-9]$",
			},
			"duration": map[string]interface{}{
				"type":    "integer",
				"minimum": 15,
			},
			"location": map[string]interface{}{
				"type": "string",
			},
			"attendees": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name":  map[string]interface{}{"type": "string"},
						"email": map[string]interface{}{"type": "string", "format": "email"},
					},
					"required": []string{"name", "email"},
				},
			},
			"reminders": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type":    "integer",
					"minimum": 0,
				},
			},
		},
		"required": []string{"title", "date", "time", "duration"},
		"additionalProperties": false,
	})

	// OpenAI's structured outputs use response_format with JSON schema
	result, err := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
		Model:  model,
		Prompt: "Create a calendar event for a team standup meeting tomorrow at 10 AM for 30 minutes with 3 attendees",
		Schema: eventSchema,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	jsonBytes, _ := json.MarshalIndent(result.Object, "", "  ")
	fmt.Println(string(jsonBytes))
	fmt.Printf("\nToken usage: %d\n", result.Usage.TotalTokens)
}

func generateAPIResponse(ctx context.Context, model provider.LanguageModel) {
	apiSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"status": map[string]interface{}{
				"type": "string",
				"enum": []string{"success", "error", "pending"},
			},
			"code": map[string]interface{}{
				"type":    "integer",
				"minimum": 100,
				"maximum": 599,
			},
			"message": map[string]interface{}{
				"type": "string",
			},
			"data": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type": "string",
					},
					"timestamp": map[string]interface{}{
						"type":    "integer",
						"minimum": 0,
					},
					"result": map[string]interface{}{
						"type": "object",
					},
				},
			},
			"errors": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"field":   map[string]interface{}{"type": "string"},
						"message": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
		"required": []string{"status", "code", "message"},
	})

	result, err := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate a successful API response for a user creation request with user ID 'usr_123'",
		Schema: apiSchema,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	jsonBytes, _ := json.MarshalIndent(result.Object, "", "  ")
	fmt.Println(string(jsonBytes))
}

func extractStructuredData(ctx context.Context, model provider.LanguageModel) {
	extractionSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"invoice": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"invoiceNumber": map[string]interface{}{"type": "string"},
					"date":          map[string]interface{}{"type": "string"},
					"dueDate":       map[string]interface{}{"type": "string"},
					"vendor": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"name":    map[string]interface{}{"type": "string"},
							"address": map[string]interface{}{"type": "string"},
							"phone":   map[string]interface{}{"type": "string"},
						},
					},
					"customer": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"name":    map[string]interface{}{"type": "string"},
							"address": map[string]interface{}{"type": "string"},
						},
					},
					"items": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"description": map[string]interface{}{"type": "string"},
								"quantity":    map[string]interface{}{"type": "integer"},
								"unitPrice":   map[string]interface{}{"type": "number"},
								"total":       map[string]interface{}{"type": "number"},
							},
						},
					},
					"subtotal": map[string]interface{}{"type": "number"},
					"tax":      map[string]interface{}{"type": "number"},
					"total":    map[string]interface{}{"type": "number"},
				},
			},
		},
	})

	invoiceText := `
INVOICE #INV-2024-001
Date: March 15, 2024
Due Date: April 15, 2024

From:
Acme Corp
123 Business St
San Francisco, CA 94105
Phone: (555) 123-4567

To:
TechStart Inc
456 Startup Ave
Palo Alto, CA 94301

Items:
1. Software License - Qty: 10 - $99.00 each - $990.00
2. Support Package - Qty: 1 - $500.00 each - $500.00
3. Training Session - Qty: 2 - $250.00 each - $500.00

Subtotal: $1,990.00
Tax (8.5%): $169.15
Total: $2,159.15
`

	result, err := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
		Model:  model,
		Prompt: fmt.Sprintf("Extract all information from this invoice into structured format:\n\n%s", invoiceText),
		Schema: extractionSchema,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	jsonBytes, _ := json.MarshalIndent(result.Object, "", "  ")
	fmt.Println(string(jsonBytes))
	fmt.Printf("\nFinish reason: %s\n", result.FinishReason)
}

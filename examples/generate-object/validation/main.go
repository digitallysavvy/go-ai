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

// User represents a user profile with validation
type User struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Age      int    `json:"age"`
	Role     string `json:"role"`
	IsActive bool   `json:"isActive"`
}

// ProductReview represents a product review with constraints
type ProductReview struct {
	ProductName string   `json:"productName"`
	Rating      int      `json:"rating"`  // 1-5
	Title       string   `json:"title"`   // Max 100 chars
	Comment     string   `json:"comment"` // Max 500 chars
	Pros        []string `json:"pros"`    // At least 1
	Cons        []string `json:"cons"`    // At least 1
	Verified    bool     `json:"verified"`
	Helpful     int      `json:"helpful"` // Min 0
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	p := openai.New(openai.Config{
		APIKey: apiKey,
	})

	model, err := p.LanguageModel("gpt-4o-mini")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Example 1: User Profile with Validation ===")
	generateUserProfile(ctx, model)

	fmt.Println("\n=== Example 2: Product Review with Constraints ===")
	generateProductReview(ctx, model)

	fmt.Println("\n=== Example 3: Email Classification with Enum ===")
	classifyEmail(ctx, model)
}

func generateUserProfile(ctx context.Context, model provider.LanguageModel) {
	// Strict schema with type validation
	userSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]interface{}{
				"type":        "string",
				"pattern":     "^[A-Z]{3}-[0-9]{6}$",
				"description": "User ID in format XXX-######",
			},
			"name": map[string]interface{}{
				"type":      "string",
				"minLength": 2,
				"maxLength": 50,
			},
			"email": map[string]interface{}{
				"type":   "string",
				"format": "email",
			},
			"age": map[string]interface{}{
				"type":    "integer",
				"minimum": 18,
				"maximum": 120,
			},
			"role": map[string]interface{}{
				"type": "string",
				"enum": []string{"admin", "user", "moderator", "guest"},
			},
			"isActive": map[string]interface{}{
				"type": "boolean",
			},
		},
		"required":             []string{"id", "name", "email", "age", "role", "isActive"},
		"additionalProperties": false,
	})

	result, err := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate a user profile for John Smith, age 28, who is a moderator",
		Schema: userSchema,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	var user User
	jsonBytes, _ := json.Marshal(result.Object)
	if err := json.Unmarshal(jsonBytes, &user); err != nil {
		log.Printf("Error parsing user: %v", err)
		return
	}

	fmt.Printf("User Profile:\n")
	fmt.Printf("  ID: %s\n", user.ID)
	fmt.Printf("  Name: %s\n", user.Name)
	fmt.Printf("  Email: %s\n", user.Email)
	fmt.Printf("  Age: %d\n", user.Age)
	fmt.Printf("  Role: %s\n", user.Role)
	fmt.Printf("  Active: %v\n", user.IsActive)
	fmt.Printf("\nTokens used: %d\n", result.Usage.TotalTokens)
}

func generateProductReview(ctx context.Context, model provider.LanguageModel) {
	reviewSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"productName": map[string]interface{}{
				"type":      "string",
				"minLength": 1,
			},
			"rating": map[string]interface{}{
				"type":    "integer",
				"minimum": 1,
				"maximum": 5,
			},
			"title": map[string]interface{}{
				"type":      "string",
				"minLength": 5,
				"maxLength": 100,
			},
			"comment": map[string]interface{}{
				"type":      "string",
				"minLength": 20,
				"maxLength": 500,
			},
			"pros": map[string]interface{}{
				"type":     "array",
				"items":    map[string]interface{}{"type": "string"},
				"minItems": 1,
				"maxItems": 5,
			},
			"cons": map[string]interface{}{
				"type":     "array",
				"items":    map[string]interface{}{"type": "string"},
				"minItems": 1,
				"maxItems": 5,
			},
			"verified": map[string]interface{}{
				"type": "boolean",
			},
			"helpful": map[string]interface{}{
				"type":    "integer",
				"minimum": 0,
			},
		},
		"required": []string{"productName", "rating", "title", "comment", "pros", "cons", "verified", "helpful"},
	})

	result, err := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate a 4-star review for a wireless keyboard. It should be balanced with both positives and negatives.",
		Schema: reviewSchema,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	var review ProductReview
	jsonBytes, _ := json.Marshal(result.Object)
	if err := json.Unmarshal(jsonBytes, &review); err != nil {
		log.Printf("Error parsing review: %v", err)
		return
	}

	fmt.Printf("Product Review:\n")
	fmt.Printf("Product: %s\n", review.ProductName)
	fmt.Printf("Rating: %d/5 ⭐\n", review.Rating)
	fmt.Printf("Title: %s\n", review.Title)
	fmt.Printf("Comment: %s\n", review.Comment)
	fmt.Printf("\nPros:\n")
	for _, pro := range review.Pros {
		fmt.Printf("  + %s\n", pro)
	}
	fmt.Printf("\nCons:\n")
	for _, con := range review.Cons {
		fmt.Printf("  - %s\n", con)
	}
	fmt.Printf("\nVerified Purchase: %v\n", review.Verified)
	fmt.Printf("Helpful votes: %d\n", review.Helpful)
}

func classifyEmail(ctx context.Context, model provider.LanguageModel) {
	// Use enum mode for classification
	classificationSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"category": map[string]interface{}{
				"type": "string",
				"enum": []string{"urgent", "important", "normal", "spam", "promotional"},
			},
			"sentiment": map[string]interface{}{
				"type": "string",
				"enum": []string{"positive", "neutral", "negative"},
			},
			"priority": map[string]interface{}{
				"type":    "integer",
				"minimum": 1,
				"maximum": 5,
			},
			"requiresResponse": map[string]interface{}{
				"type": "boolean",
			},
			"topics": map[string]interface{}{
				"type":     "array",
				"items":    map[string]interface{}{"type": "string"},
				"minItems": 1,
				"maxItems": 3,
			},
		},
		"required": []string{"category", "sentiment", "priority", "requiresResponse", "topics"},
	})

	emailText := `
	Subject: URGENT: Server Downtime Issue

	Hi Team,

	We're experiencing critical server downtime affecting production.
	Multiple customers are reporting 503 errors. This needs immediate attention!

	Please respond ASAP.
	`

	result, err := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
		Model:  model,
		Prompt: fmt.Sprintf("Classify this email and extract key information:\n%s", emailText),
		Schema: classificationSchema,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	jsonBytes, _ := json.MarshalIndent(result.Object, "", "  ")
	fmt.Println("Email Classification:")
	fmt.Println(string(jsonBytes))
	fmt.Printf("\nFinish reason: %s\n", result.FinishReason)
}

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// Route represents an intent route
type Route struct {
	Name        string
	Description string
	Examples    []string
	Handler     func(context.Context, string) string
}

// SemanticRouter routes user queries to appropriate handlers
type SemanticRouter struct{
	routes []Route
	model  provider.LanguageModel
}

func NewSemanticRouter(model provider.LanguageModel) *SemanticRouter {
	return &SemanticRouter{
		routes: []Route{},
		model:  model,
	}
}

func (sr *SemanticRouter) AddRoute(route Route) {
	sr.routes = append(sr.routes, route)
}

func (sr *SemanticRouter) Route(ctx context.Context, query string) (string, error) {
	// Step 1: Classify intent
	intent, confidence := sr.classifyIntent(ctx, query)

	fmt.Printf("🔍 Detected intent: %s (confidence: %.2f)\n\n", intent, confidence)

	// Step 2: Find matching route
	for _, route := range sr.routes {
		if route.Name == intent {
			return route.Handler(ctx, query), nil
		}
	}

	return "No matching route found for intent: " + intent, nil
}

func (sr *SemanticRouter) classifyIntent(ctx context.Context, query string) (string, float64) {
	// Build classification prompt
	routeDescriptions := ""
	for _, route := range sr.routes {
		routeDescriptions += fmt.Sprintf("\n%s: %s\nExamples: %s\n",
			route.Name, route.Description, strings.Join(route.Examples, ", "))
	}

	prompt := fmt.Sprintf(`Classify this user query into one of these intents:
%s

User query: "%s"

Respond with ONLY the intent name.`, routeDescriptions, query)

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  sr.model,
		Prompt: prompt,
	})

	if err != nil {
		return "unknown", 0.0
	}

	intent := strings.TrimSpace(result.Text)
	confidence := 0.85 // Simplified confidence score

	return intent, confidence
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, _ := p.LanguageModel("gpt-4")

	ctx := context.Background()

	fmt.Println("=== Semantic Router Example ===")

	// Create router
	router := NewSemanticRouter(model)

	// Define routes
	router.AddRoute(Route{
		Name:        "greeting",
		Description: "User is greeting or saying hello",
		Examples:    []string{"hi", "hello", "hey there", "good morning"},
		Handler: func(ctx context.Context, query string) string {
			return "👋 Hello! How can I help you today?"
		},
	})

	router.AddRoute(Route{
		Name:        "technical_support",
		Description: "User has a technical problem or needs troubleshooting help",
		Examples:    []string{"my app crashed", "error 500", "can't login", "bug report"},
		Handler: func(ctx context.Context, query string) string {
			return "🔧 I'll help you troubleshoot. Let me gather some information about the issue..."
		},
	})

	router.AddRoute(Route{
		Name:        "product_inquiry",
		Description: "User is asking about products, features, or pricing",
		Examples:    []string{"what features do you have", "how much does it cost", "tell me about your product"},
		Handler: func(ctx context.Context, query string) string {
			return "📦 I'd be happy to tell you about our products! Here's what we offer..."
		},
	})

	router.AddRoute(Route{
		Name:        "feedback",
		Description: "User is providing feedback, suggestions, or complaints",
		Examples:    []string{"I love this feature", "you should add...", "this is terrible"},
		Handler: func(ctx context.Context, query string) string {
			return "💭 Thank you for your feedback! We value your input and will consider it..."
		},
	})

	router.AddRoute(Route{
		Name:        "account_management",
		Description: "User wants to manage their account, subscription, or billing",
		Examples:    []string{"cancel subscription", "update payment", "delete my account", "change password"},
		Handler: func(ctx context.Context, query string) string {
			return "👤 I can help with your account. For security, let me verify your identity first..."
		},
	})

	router.AddRoute(Route{
		Name:        "documentation",
		Description: "User is looking for documentation, guides, or how-to information",
		Examples:    []string{"how do I...", "documentation for...", "tutorial on..."},
		Handler: func(ctx context.Context, query string) string {
			return "📚 Let me find the relevant documentation for you..."
		},
	})

	// Test queries
	testQueries := []string{
		"Hey! How's it going?",
		"I'm getting a 404 error when I try to access my dashboard",
		"What's included in the premium plan?",
		"This new UI is amazing! Great job!",
		"I need to update my credit card information",
		"Can you show me how to export data?",
		"The app keeps crashing when I upload files",
		"How much does the enterprise version cost?",
	}

	// Route each query
	for i, query := range testQueries {
		fmt.Printf("=== Test %d ===\n", i+1)
		fmt.Printf("Query: \"%s\"\n\n", query)

		response, err := router.Route(ctx, query)
		if err != nil {
			fmt.Printf("Error: %v\n\n", err)
			continue
		}

		fmt.Printf("Response: %s\n", response)
		fmt.Println(strings.Repeat("-", 60))
		fmt.Println()
	}

	// Example 2: Multi-level routing
	fmt.Println("\n=== Example 2: Multi-Level Routing ===")

	multiLevelRouter := NewSemanticRouter(model)

	multiLevelRouter.AddRoute(Route{
		Name:        "api_question",
		Description: "Questions about API usage, endpoints, authentication",
		Examples:    []string{"how do I authenticate", "what's the rate limit", "API documentation"},
		Handler: func(ctx context.Context, query string) string {
			// Sub-router for API questions
			subRouter := NewSemanticRouter(model)

			subRouter.AddRoute(Route{
				Name:        "authentication",
				Description: "API authentication questions",
				Examples:    []string{"how to auth", "get API key", "bearer token"},
				Handler: func(ctx context.Context, q string) string {
					return "🔐 API Authentication: Use Bearer token in Authorization header..."
				},
			})

			subRouter.AddRoute(Route{
				Name:        "rate_limits",
				Description: "API rate limiting questions",
				Examples:    []string{"rate limit", "how many requests", "throttling"},
				Handler: func(ctx context.Context, q string) string {
					return "⏱️ Rate Limits: 100 requests per minute for free tier..."
				},
			})

			result, _ := subRouter.Route(ctx, query)
			return result
		},
	})

	apiQuery := "What are the API rate limits for the free plan?"
	fmt.Printf("Query: \"%s\"\n\n", apiQuery)
	response, _ := multiLevelRouter.Route(ctx, apiQuery)
	fmt.Printf("Response: %s\n\n", response)

	// Example 3: Dynamic routing with context
	fmt.Println("=== Example 3: Context-Aware Routing ===")

	contextQuery := "I need help"
	userContext := "Currently viewing billing page"

	fmt.Printf("Query: \"%s\"\n", contextQuery)
	fmt.Printf("Context: %s\n\n", userContext)

	contextPrompt := fmt.Sprintf(`Given the user context, route this query:

Context: %s
Query: %s

Likely intent (choose one):
- billing_support (user needs billing help)
- general_support (general help request)
- navigation_help (user is lost)

Respond with intent only.`, userContext, contextQuery)

	result, _ := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: contextPrompt,
	})

	fmt.Printf("Context-aware intent: %s\n", result.Text)

	fmt.Println("\n=== Semantic Router Benefits ===")
	benefits := []string{
		"✓ Intent-based routing (not keyword matching)",
		"✓ Handles natural language variations",
		"✓ Easy to add new routes",
		"✓ Context-aware routing",
		"✓ Multi-level routing support",
		"✓ Confidence scoring",
	}
	for _, benefit := range benefits {
		fmt.Println("  " + benefit)
	}

	fmt.Println("\n=== Use Cases ===")
	useCases := []string{
		"• Customer support chatbots",
		"• Multi-agent systems",
		"• Intent classification",
		"• Conversational interfaces",
		"• Smart routing in microservices",
		"• Dynamic workflow routing",
	}
	for _, useCase := range useCases {
		fmt.Println("  " + useCase)
	}

	fmt.Println("\n=== Advanced Patterns ===")
	patterns := map[string]string{
		"Fallback routing":    "Route to default handler if confidence < threshold",
		"Hybrid routing":      "Combine semantic + rule-based routing",
		"A/B testing routes":  "Route to different handlers for experimentation",
		"Priority routing":    "High-priority intents bypass queue",
		"Regional routing":    "Route based on user location/language",
	}

	for pattern, description := range patterns {
		fmt.Printf("  • %s: %s\n", pattern, description)
	}
}

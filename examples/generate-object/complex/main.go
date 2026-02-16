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

	model, err := p.LanguageModel("gpt-4o")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	fmt.Println("=== Example 1: Complex Nested Company Structure ===")
	generateCompanyStructure(ctx, model)

	fmt.Println("\n=== Example 2: Deep E-commerce Order ===")
	generateEcommerceOrder(ctx, model)

	fmt.Println("\n=== Example 3: Course Curriculum with Optional Fields ===")
	generateCourseCurriculum(ctx, model)
}

func generateCompanyStructure(ctx context.Context, model provider.LanguageModel) {
	// Deeply nested organizational structure
	companySchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"company": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name":    map[string]interface{}{"type": "string"},
					"founded": map[string]interface{}{"type": "integer"},
					"headquarters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"city":    map[string]interface{}{"type": "string"},
							"country": map[string]interface{}{"type": "string"},
							"address": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"street":  map[string]interface{}{"type": "string"},
									"zipCode": map[string]interface{}{"type": "string"},
								},
							},
						},
					},
					"departments": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name": map[string]interface{}{"type": "string"},
								"head": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"name":  map[string]interface{}{"type": "string"},
										"email": map[string]interface{}{"type": "string"},
										"phone": map[string]interface{}{"type": "string"},
									},
								},
								"teams": map[string]interface{}{
									"type": "array",
									"items": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"name": map[string]interface{}{"type": "string"},
											"members": map[string]interface{}{
												"type": "array",
												"items": map[string]interface{}{
													"type": "object",
													"properties": map[string]interface{}{
														"name":  map[string]interface{}{"type": "string"},
														"role":  map[string]interface{}{"type": "string"},
														"email": map[string]interface{}{"type": "string"},
														"seniority": map[string]interface{}{
															"type": "string",
															"enum": []string{"junior", "mid", "senior", "lead"},
														},
													},
												},
											},
											"projects": map[string]interface{}{
												"type": "array",
												"items": map[string]interface{}{
													"type": "object",
													"properties": map[string]interface{}{
														"name": map[string]interface{}{"type": "string"},
														"status": map[string]interface{}{
															"type": "string",
															"enum": []string{"planning", "active", "completed", "on-hold"},
														},
														"budget":   map[string]interface{}{"type": "number"},
														"deadline": map[string]interface{}{"type": "string"},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	})

	result, err := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate a tech company structure with 2 departments (Engineering and Product), each with 2 teams",
		Schema: companySchema,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	jsonBytes, _ := json.MarshalIndent(result.Object, "", "  ")
	fmt.Println(string(jsonBytes))
	fmt.Printf("\nTokens used: %d\n", result.Usage.GetTotalTokens())
}

func generateEcommerceOrder(ctx context.Context, model provider.LanguageModel) {
	orderSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"order": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"orderId":   map[string]interface{}{"type": "string"},
					"orderDate": map[string]interface{}{"type": "string"},
					"orderStatus": map[string]interface{}{
						"type": "string",
						"enum": []string{"pending", "processing", "shipped", "delivered", "cancelled"},
					},
					"customer": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"customerId": map[string]interface{}{"type": "string"},
							"name":       map[string]interface{}{"type": "string"},
							"email":      map[string]interface{}{"type": "string"},
							"phone":      map[string]interface{}{"type": "string"},
							"addresses": map[string]interface{}{
								"type": "array",
								"items": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"type": map[string]interface{}{
											"type": "string",
											"enum": []string{"billing", "shipping"},
										},
										"street":  map[string]interface{}{"type": "string"},
										"city":    map[string]interface{}{"type": "string"},
										"state":   map[string]interface{}{"type": "string"},
										"zipCode": map[string]interface{}{"type": "string"},
										"country": map[string]interface{}{"type": "string"},
									},
								},
							},
							"paymentMethods": map[string]interface{}{
								"type": "array",
								"items": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"type": map[string]interface{}{
											"type": "string",
											"enum": []string{"credit_card", "debit_card", "paypal", "apple_pay"},
										},
										"last4": map[string]interface{}{"type": "string"},
									},
								},
							},
						},
					},
					"items": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"productId":   map[string]interface{}{"type": "string"},
								"productName": map[string]interface{}{"type": "string"},
								"category":    map[string]interface{}{"type": "string"},
								"quantity":    map[string]interface{}{"type": "integer", "minimum": 1},
								"unitPrice":   map[string]interface{}{"type": "number"},
								"discount": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"amount":     map[string]interface{}{"type": "number"},
										"percentage": map[string]interface{}{"type": "number"},
										"code":       map[string]interface{}{"type": "string"},
									},
								},
								"subtotal": map[string]interface{}{"type": "number"},
							},
						},
					},
					"pricing": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"subtotal": map[string]interface{}{"type": "number"},
							"tax":      map[string]interface{}{"type": "number"},
							"shipping": map[string]interface{}{"type": "number"},
							"discount": map[string]interface{}{"type": "number"},
							"total":    map[string]interface{}{"type": "number"},
							"currency": map[string]interface{}{"type": "string"},
						},
					},
					"shipping": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"method":            map[string]interface{}{"type": "string"},
							"carrier":           map[string]interface{}{"type": "string"},
							"trackingNumber":    map[string]interface{}{"type": "string"},
							"estimatedDelivery": map[string]interface{}{"type": "string"},
						},
					},
				},
			},
		},
	})

	result, err := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate a complete e-commerce order for a customer buying 3 different electronics items with shipping",
		Schema: orderSchema,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	jsonBytes, _ := json.MarshalIndent(result.Object, "", "  ")
	fmt.Println(string(jsonBytes))
}

func generateCourseCurriculum(ctx context.Context, model provider.LanguageModel) {
	// Schema with optional fields using oneOf, anyOf
	curriculumSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"course": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"title":   map[string]interface{}{"type": "string"},
					"code":    map[string]interface{}{"type": "string"},
					"credits": map[string]interface{}{"type": "integer"},
					"level": map[string]interface{}{
						"type": "string",
						"enum": []string{"beginner", "intermediate", "advanced"},
					},
					"prerequisites": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string"},
					},
					"instructor": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"name":        map[string]interface{}{"type": "string"},
							"email":       map[string]interface{}{"type": "string"},
							"officeHours": map[string]interface{}{"type": "string"},
							"bio":         map[string]interface{}{"type": "string"},
						},
					},
					"modules": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"moduleNumber": map[string]interface{}{"type": "integer"},
								"title":        map[string]interface{}{"type": "string"},
								"description":  map[string]interface{}{"type": "string"},
								"duration":     map[string]interface{}{"type": "string"},
								"lessons": map[string]interface{}{
									"type": "array",
									"items": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"lessonNumber": map[string]interface{}{"type": "integer"},
											"title":        map[string]interface{}{"type": "string"},
											"type": map[string]interface{}{
												"type": "string",
												"enum": []string{"video", "reading", "quiz", "assignment", "lab"},
											},
											"duration": map[string]interface{}{"type": "string"},
											"resources": map[string]interface{}{
												"type": "array",
												"items": map[string]interface{}{
													"type": "object",
													"properties": map[string]interface{}{
														"title": map[string]interface{}{"type": "string"},
														"type": map[string]interface{}{
															"type": "string",
															"enum": []string{"video", "pdf", "link", "code"},
														},
														"url": map[string]interface{}{"type": "string"},
													},
												},
											},
										},
									},
								},
								"assessment": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"type": map[string]interface{}{
											"type": "string",
											"enum": []string{"quiz", "exam", "project", "presentation"},
										},
										"weight":   map[string]interface{}{"type": "integer"},
										"duration": map[string]interface{}{"type": "string"},
									},
								},
							},
						},
					},
					"grading": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"scale": map[string]interface{}{
								"type": "array",
								"items": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"grade":      map[string]interface{}{"type": "string"},
										"minPercent": map[string]interface{}{"type": "integer"},
										"maxPercent": map[string]interface{}{"type": "integer"},
									},
								},
							},
							"components": map[string]interface{}{
								"type": "array",
								"items": map[string]interface{}{
									"type": "object",
									"properties": map[string]interface{}{
										"name":   map[string]interface{}{"type": "string"},
										"weight": map[string]interface{}{"type": "integer"},
									},
								},
							},
						},
					},
				},
			},
		},
	})

	result, err := ai.GenerateObject(ctx, ai.GenerateObjectOptions{
		Model:  model,
		Prompt: "Generate a complete computer science course curriculum on 'Introduction to Machine Learning' with 3 modules",
		Schema: curriculumSchema,
	})
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	jsonBytes, _ := json.MarshalIndent(result.Object, "", "  ")
	fmt.Println(string(jsonBytes))
	fmt.Printf("\nFinish reason: %s\n", result.FinishReason)
}

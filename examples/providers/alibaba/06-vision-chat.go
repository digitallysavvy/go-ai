package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
)

// Example 6: Vision Chat with Qwen VL
// This example demonstrates image understanding using Alibaba's vision-language model

func main() {
	// Create Alibaba provider
	cfg, err := alibaba.NewConfig(os.Getenv("ALIBABA_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	prov := alibaba.New(cfg)

	// Use the vision-language model (qwen-vl-max)
	model, err := prov.LanguageModel("qwen-vl-max")
	if err != nil {
		log.Fatal(err)
	}

	// Create a prompt with an image
	// This example uses a URL, but you can also use base64-encoded images
	imageURL := "https://example.com/photo.jpg"

	prompt := types.Prompt{
		Messages: []types.Message{
			{
				Role: "user",
				Content: []types.ContentPart{
					{
						Type: "image",
						Image: &types.ImagePart{
							Type: "url",
							URL:  imageURL,
						},
					},
					{
						Type: "text",
						Text: "What do you see in this image? Describe it in detail.",
					},
				},
			},
		},
	}

	// Generate response
	ctx := context.Background()
	result, err := model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: prompt,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Print the result
	fmt.Println("Vision Analysis:")
	fmt.Println(result.Text)
	fmt.Println()

	// Multi-turn conversation with vision
	fmt.Println("\n--- Multi-turn Vision Chat ---\n")

	// Add the assistant's response to the conversation
	messages := append(prompt.Messages, types.Message{
		Role:    "assistant",
		Content: []types.ContentPart{{Type: "text", Text: result.Text}},
	})

	// Ask a follow-up question
	messages = append(messages, types.Message{
		Role:    "user",
		Content: []types.ContentPart{{Type: "text", Text: "What colors are dominant in this image?"}},
	})

	prompt2 := types.Prompt{Messages: messages}
	result2, err := model.DoGenerate(ctx, &provider.GenerateOptions{
		Prompt: prompt2,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Follow-up Response:")
	fmt.Println(result2.Text)
	fmt.Println()

	// Print token usage
	fmt.Printf("Token Usage:\n")
	fmt.Printf("  Input:  %d tokens\n", result2.Usage.GetInputTokens())
	fmt.Printf("  Output: %d tokens\n", result2.Usage.GetOutputTokens())
	fmt.Printf("  Total:  %d tokens\n", result2.Usage.GetTotalTokens())
}

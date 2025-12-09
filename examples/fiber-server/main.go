package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

var model provider.LanguageModel

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, _ = p.LanguageModel("gpt-4")

	app := fiber.New(fiber.Config{
		AppName: "Go AI Fiber Server",
	})

	app.Use(logger.New())
	app.Use(cors.New())

	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"service": "Go AI Fiber Server",
			"version": "1.0.0",
		})
	})

	app.Post("/generate", handleGenerate)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("🚀 Fiber server on :%s\n", port)
	log.Fatal(app.Listen(":" + port))
}

func handleGenerate(c *fiber.Ctx) error {
	var req struct {
		Prompt string `json:"prompt"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := ai.GenerateText(ctx, ai.GenerateTextOptions{
		Model:  model,
		Prompt: req.Prompt,
	})

	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"text":  result.Text,
		"usage": result.Usage,
	})
}

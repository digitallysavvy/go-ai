package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

var model provider.LanguageModel

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})
	model, _ = p.LanguageModel("gpt-4")

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST"},
	}))

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"service": "Go AI Chi Server",
			"version": "1.0.0",
		})
	})

	r.Post("/generate", handleGenerate)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("🚀 Chi server on :%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

func handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt string `json:"prompt"`
	}

	json.NewDecoder(r.Body).Decode(&req)

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: req.Prompt,
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"text":  result.Text,
		"usage": result.Usage,
	})
}

// Recipe: serve a useChat frontend from Go.
//
// Run: ANTHROPIC_API_KEY=... go run ./examples/recipes/usechat-backend
// Then point useChat's DefaultChatTransport at http://localhost:8080/api/chat.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func main() {
	model, err := anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")}).
		LanguageModel(anthropic.ClaudeSonnet5_5)
	if err != nil {
		log.Fatal(err)
	}
	assistant := agent.NewToolLoopAgent(agent.AgentConfig{
		Model:  model,
		System: "You are a concise assistant.",
	})

	http.HandleFunc("POST /api/chat", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// Validate and convert the UI messages before anything is written, so
		// a bad request gets a plain 400.
		chunks, _, err := agent.CreateAgentUIStreamFromUIMessages(r.Context(), assistant,
			agent.CreateAgentUIStreamFromUIMessagesOptions{UIMessages: []byte(req.Messages)})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Sets the SSE headers, writes 200 and flushes each chunk.
		keepAlive := 15 * time.Second
		if err := ai.PipeUIMessageChunksToResponse(chunks, w, &ai.UIMessageStreamResponseInit{KeepAliveMs: &keepAlive}); err != nil {
			log.Printf("write stream: %v", err)
		}
	})

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

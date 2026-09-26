// Every snippet here is a complete program that was compiled with `go build`
// against the current module before being placed on the landing page.
// Keep the code byte-for-byte in sync with what was verified.

export type Snippet = {
  id: string;
  pkg: string;
  symbol: string;
  file: string;
  summary: string;
  docs: string;
  docsLabel: string;
  code: string;
};

export const SHOWCASE: Snippet[] = [
  {
    id: 'generate',
    pkg: 'ai.',
    symbol: 'GenerateText',
    file: 'main.go',
    summary:
      'Prompt in, text out. The result also carries usage, steps and tool calls when there are any.',
    docs: '/docs/ai-sdk-core/generating-text',
    docsLabel: 'Generating text',
    code: `package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func main() {
	p := anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")})
	model, err := p.LanguageModel(anthropic.ClaudeSonnet4_6)
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "Explain Go's context package in two sentences.",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(result.Text)
}
`,
  },
  {
    id: 'stream',
    pkg: 'ai.',
    symbol: 'StreamText',
    file: 'main.go',
    summary:
      'Chunks arrive on a channel. Range over them and print as the model writes; Close when you are done.',
    docs: '/docs/ai-sdk-core/generating-text',
    docsLabel: 'Streaming',
    code: `package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	p := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})
	model, err := p.LanguageModel(openai.ModelGPT5Mini)
	if err != nil {
		log.Fatal(err)
	}

	stream, err := ai.StreamText(context.Background(), ai.StreamTextOptions{
		Model:  model,
		Prompt: "Write a limerick about goroutines.",
	})
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	for chunk := range stream.Chunks() {
		if chunk.Type == provider.ChunkTypeText {
			fmt.Print(chunk.Text)
		}
	}
}
`,
  },
  {
    id: 'agent',
    pkg: 'agent.',
    symbol: 'ToolLoopAgent',
    file: 'main.go',
    summary:
      'A tool is a name, a JSON schema and an Execute func. The agent loops through tool calls until a StopWhen condition is met.',
    docs: '/docs/agents/overview',
    docsLabel: 'Agents',
    code: `package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

func main() {
	p := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})
	model, err := p.LanguageModel(openai.ModelGPT5Mini)
	if err != nil {
		log.Fatal(err)
	}

	weather := types.Tool{
		Name:        "getWeather",
		Description: "Current weather for a city.",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
			"required":   []string{"city"},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"city": input["city"], "tempC": 17, "rain": true}, nil
		},
	}

	a := agent.NewToolLoopAgent(agent.AgentConfig{
		Model:    model,
		System:   "You are a terse travel assistant.",
		Tools:    []types.Tool{weather},
		StopWhen: []ai.StopCondition{ai.StepCountIs(5)},
	})

	res, err := a.Execute(context.Background(), "Do I need a jacket in Lisbon today?")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Text)
}
`,
  },
  {
    id: 'object',
    pkg: 'ai.',
    symbol: 'ObjectOutput',
    file: 'main.go',
    summary:
      'Describe the output as a Go struct. SchemaFor derives the JSON schema and result.Output comes back as that struct.',
    docs: '/docs/ai-sdk-core/generating-structured-data',
    docsLabel: 'Structured output',
    code: `package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

type Recipe struct {
	Name        string   \`json:"name"\`
	Ingredients []string \`json:"ingredients"\`
	Steps       []string \`json:"steps"\`
}

func main() {
	p := openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")})
	model, err := p.LanguageModel(openai.ModelGPT5Mini)
	if err != nil {
		log.Fatal(err)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: "A weeknight recipe for pão de queijo.",
		Output: ai.ObjectOutput[Recipe](ai.ObjectOutputOptions{
			Schema: ai.SchemaFor[Recipe](),
		}),
	})
	if err != nil {
		log.Fatal(err)
	}

	recipe := result.Output.(Recipe)
	fmt.Println(recipe.Name, len(recipe.Steps), "steps")
}
`,
  },
];

export const UI_STREAM_SNIPPET = `package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
)

func main() {
	p := anthropic.New(anthropic.Config{APIKey: os.Getenv("ANTHROPIC_API_KEY")})
	model, err := p.LanguageModel(anthropic.ClaudeSonnet4_6)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []ai.UIMessage \`json:"messages"\`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		messages, err := ai.ConvertToModelMessages(r.Context(), body.Messages)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		stream, err := ai.StreamText(r.Context(), ai.StreamTextOptions{
			Model:    model,
			Messages: messages,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Vercel-AI-UI-Message-Stream", "v1")
		if err := ai.PipeUIMessageStreamToResponse(r.Context(), stream, w); err != nil {
			log.Println(err)
		}
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}
`;

export const INSTALL_COMMAND = 'go get github.com/digitallysavvy/go-ai';

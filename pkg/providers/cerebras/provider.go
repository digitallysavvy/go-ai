package cerebras

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// Provider implements the provider.Provider interface for Cerebras
// Cerebras is OpenAI-compatible, so we use the OpenAI implementation
type Provider struct {
	*openai.Provider
}

// Config contains configuration for the Cerebras provider
type Config struct {
	// APIKey is the Cerebras API key
	APIKey string

	// BaseURL is the base URL for the Cerebras API (optional)
	BaseURL string

	// HTTPClient overrides the HTTP client used for requests.
	HTTPClient *http.Client
}

// New creates a new Cerebras provider
// Cerebras uses OpenAI-compatible API
func New(cfg Config) *Provider {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.cerebras.ai/v1"
	}

	openaiProvider := openai.New(openai.Config{
		APIKey:     cfg.APIKey,
		Name:       "cerebras",
		BaseURL:    baseURL,
		HTTPClient: withCerebrasTransform(cfg.HTTPClient),
	})

	return &Provider{
		Provider: openaiProvider,
	}
}

// LanguageModel returns a Cerebras language model with Cerebras-specific
// request/response compatibility fixes layered over the OpenAI-compatible API.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	base, err := p.Provider.LanguageModel(modelID)
	if err != nil {
		return nil, err
	}
	return &LanguageModel{base: base}, nil
}

func withCerebrasTransform(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{}
	} else {
		clone := *client
		client = &clone
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = cerebrasTransformTransport{base: base}
	return client
}

type cerebrasTransformTransport struct {
	base http.RoundTripper
}

func (t cerebrasTransformTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body == nil {
		return t.base.RoundTrip(req)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	var payload interface{}
	if err := json.Unmarshal(body, &payload); err == nil {
		renameReasoningContent(payload)
		if transformed, err := json.Marshal(payload); err == nil {
			body = transformed
		}
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	return t.base.RoundTrip(req)
}

func renameReasoningContent(value interface{}) {
	payload, ok := value.(map[string]interface{})
	if !ok {
		return
	}
	messages, ok := payload["messages"].([]interface{})
	if !ok {
		return
	}
	for _, message := range messages {
		msg, ok := message.(map[string]interface{})
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role != "assistant" {
			continue
		}
		reasoning, hasReasoningContent := msg["reasoning_content"]
		if !hasReasoningContent {
			continue
		}
		delete(msg, "reasoning_content")
		if _, hasReasoning := msg["reasoning"]; !hasReasoning && reasoning != nil {
			msg["reasoning"] = reasoning
		}
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "cerebras"
}

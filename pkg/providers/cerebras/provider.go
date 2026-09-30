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
		// TS CerebrasChatLanguageModel extends OpenAICompatibleChatLanguageModel,
		// which supports video_url content parts (7dd9ec320c).
		AllowVideo:    true,
		UserAgentName: "cerebras",
	})

	return &Provider{
		Provider: openaiProvider,
	}
}

// LanguageModel returns a Cerebras language model with Cerebras-specific
// request/response compatibility fixes layered over the OpenAI-compatible
// Chat Completions API (/chat/completions). Cerebras has no Responses API, so
// this must not use openai.Provider.LanguageModel (which returns the
// Responses model); TS createCerebras builds an OpenAICompatibleChatLanguageModel.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	base, err := p.Provider.ChatModel(modelID)
	if err != nil {
		return nil, err
	}
	return &LanguageModel{base: base}, nil
}

// ChatModel is an alias for LanguageModel (TS provider.chat).
func (p *Provider) ChatModel(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModel(modelID)
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
		renameMaxTokens(payload)
		if extras, ok := req.Context().Value(cerebrasOptionsContextKey{}).(cerebrasRequestExtras); ok {
			applyCerebrasRequestExtras(payload, extras)
		}
		if transformed, err := json.Marshal(payload); err == nil {
			body = transformed
		}
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	return t.base.RoundTrip(req)
}

// cerebrasOptionsContextKey is the context key LanguageModel uses to hand
// resolved providerOptions.cerebras extras to cerebrasTransformTransport,
// since the OpenAI-compatible base model only ever reads
// providerOptions["openai"] and has no hook for provider-specific extras
// (TS's OpenAICompatibleChatLanguageModel supports a transformRequestBody
// hook; Go's does not, so this carries the same information across the
// request/transport boundary instead).
type cerebrasOptionsContextKey struct{}

// renameMaxTokens mirrors TS transformCerebrasRequestBody: Cerebras expects
// max_completion_tokens, not the OpenAI-compatible chat model's max_tokens.
func renameMaxTokens(value interface{}) {
	payload, ok := value.(map[string]interface{})
	if !ok {
		return
	}
	maxTokens, hasMaxTokens := payload["max_tokens"]
	if !hasMaxTokens {
		return
	}
	delete(payload, "max_tokens")
	payload["max_completion_tokens"] = maxTokens
}

// applyCerebrasRequestExtras merges providerOptions.cerebras-derived fields
// into the request body (TS transformCerebrasRequestBody). strictJsonSchema
// is not among these: the base OpenAI-compatible chat model already applies
// it to response_format.json_schema.strict when building the body (see
// resolveCerebrasOptions).
func applyCerebrasRequestExtras(value interface{}, extras cerebrasRequestExtras) {
	payload, ok := value.(map[string]interface{})
	if !ok {
		return
	}
	for key, val := range extras {
		payload[key] = val
	}
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

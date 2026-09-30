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
		// TransformRequestBody applies the structural (provider-options-
		// independent) half of TS transformCerebrasRequestBody: renaming
		// max_tokens -> max_completion_tokens and reasoning_content ->
		// reasoning. It runs before the body is captured for
		// provider.StreamRequestBody / types.StepRequest.Body, so that
		// exposed body matches what's actually sent (mirrors TS's
		// request.body reflecting the post-transform shape). The other half
		// of transformCerebrasRequestBody — merging providerOptions.cerebras
		// extras — needs per-call data buildRequestBodyWithWarnings has no
		// hook to receive (this hook's signature is intentionally the same
		// as TS's, taking only the body), so that half stays in
		// cerebrasTransformTransport, which still receives the per-call
		// extras via context (see cerebrasOptionsContextKey).
		TransformRequestBody: transformCerebrasRequestBody,
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
	extras, hasExtras := req.Context().Value(cerebrasOptionsContextKey{}).(cerebrasRequestExtras)
	if !hasExtras {
		// renameReasoningContent/renameMaxTokens already ran as
		// openai.Config.TransformRequestBody when the body was built, so
		// with no per-call extras to merge there is nothing left for the
		// transport to rewrite.
		return t.base.RoundTrip(req)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	var payload interface{}
	if err := json.Unmarshal(body, &payload); err == nil {
		applyCerebrasRequestExtras(payload, extras)
		if transformed, err := json.Marshal(payload); err == nil {
			body = transformed
		}
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	return t.base.RoundTrip(req)
}

// cerebrasOptionsContextKey is the context key LanguageModel uses to hand
// resolved providerOptions.cerebras extras to cerebrasTransformTransport.
// openai.Config.TransformRequestBody (transformCerebrasRequestBody, below)
// now covers the structural half of TS's transformRequestBody hook, but
// it's a static function set once at provider construction with no access
// to a specific call's GenerateOptions, so it can't read per-call
// providerOptions.cerebras extras. This context key carries those extras to
// cerebrasTransformTransport at the HTTP transport layer instead, which
// still has access to the per-request context.
type cerebrasOptionsContextKey struct{}

// transformCerebrasRequestBody is the openai.Config.TransformRequestBody hook
// (TS transformCerebrasRequestBody): the structural, provider-options-
// independent half of Cerebras's request rewriting. It runs inside
// buildRequestBodyWithWarnings, before the body is captured for both the
// outgoing request and the optional provider.StreamRequestBody exposure.
func transformCerebrasRequestBody(body map[string]interface{}) map[string]interface{} {
	renameReasoningContent(body)
	renameMaxTokens(body)
	return body
}

// renameMaxTokens mirrors TS transformCerebrasRequestBody: Cerebras expects
// max_completion_tokens, not the OpenAI-compatible chat model's max_tokens.
func renameMaxTokens(payload map[string]interface{}) {
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

// renameReasoningContent mirrors TS transformCerebrasRequestBody's assistant
// message rewrite: Cerebras expects reasoning history in the `reasoning`
// field, while the shared OpenAI-compatible converter serializes it as
// `reasoning_content`. body["messages"] is []map[string]interface{} when
// this runs as the body-build-time openai.Config.TransformRequestBody hook
// (prompt.ToOpenAIMessages's return type); []interface{} is also accepted
// so the same helper stays safe to reuse against a JSON-decoded body.
func renameReasoningContent(payload map[string]interface{}) {
	forEachMessageMap(payload["messages"], func(msg map[string]interface{}) {
		if role, _ := msg["role"].(string); role != "assistant" {
			return
		}
		reasoning, hasReasoningContent := msg["reasoning_content"]
		if !hasReasoningContent {
			return
		}
		delete(msg, "reasoning_content")
		if _, hasReasoning := msg["reasoning"]; !hasReasoning && reasoning != nil {
			msg["reasoning"] = reasoning
		}
	})
}

// forEachMessageMap calls fn for each message in messages, accepting either
// []map[string]interface{} (the Go request builder's own type) or
// []interface{} of map[string]interface{} (a JSON-decoded body).
func forEachMessageMap(messages interface{}, fn func(map[string]interface{})) {
	switch msgs := messages.(type) {
	case []map[string]interface{}:
		for _, msg := range msgs {
			fn(msg)
		}
	case []interface{}:
		for _, message := range msgs {
			if msg, ok := message.(map[string]interface{}); ok {
				fn(msg)
			}
		}
	}
}

// Name returns the provider name
func (p *Provider) Name() string {
	return "cerebras"
}

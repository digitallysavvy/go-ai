package baseten

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providerrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// DefaultBaseURL is Baseten's Model APIs base URL (TS defaultBaseURL).
const DefaultBaseURL = "https://inference.baseten.co/v1"

// maxEmbeddingsPerCall is Baseten's per-request embedding input limit: larger
// batches are rejected with `413 batch size N > maximum allowed batch size
// 128`. Mirrors TS MAX_EMBEDDINGS_PER_CALL.
const maxEmbeddingsPerCall = 128

// Provider implements the provider.Provider interface for Baseten.
// Baseten is OpenAI-compatible, so this builds on the OpenAI implementation.
type Provider struct {
	*openai.Provider

	// modelURL is Config.ModelURL, kept to decide chat vs. embedding routing.
	modelURL string

	// embedding is a separate OpenAI-compatible provider instance pointed at
	// the embedding endpoint (only set when ModelURL is a /sync or /sync/v1
	// URL), since embeddings use a different provider name/base URL than chat.
	embedding *openai.Provider
}

// Config contains configuration for the Baseten provider.
type Config struct {
	// APIKey is the Baseten API key. Defaults to the BASETEN_API_KEY
	// environment variable.
	APIKey string

	// BaseURL is the base URL for the Model APIs (optional).
	// Default: https://inference.baseten.co/v1
	BaseURL string

	// ModelURL is the URL for a custom model deployment (chat or
	// embeddings). If not supplied, the default Model APIs are used for
	// chat, and EmbeddingModel returns an error (Baseten has no default
	// embeddings endpoint).
	//
	// Chat requires a /sync/v1 URL; a /predict URL returns an error, since
	// that endpoint is not an OpenAI-compatible Chat Completions API.
	// Embeddings require a /sync or /sync/v1 URL.
	ModelURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string

	// HTTPClient overrides the HTTP client used for requests.
	HTTPClient *http.Client
}

// New creates a new Baseten provider. Baseten uses an OpenAI-compatible API.
func New(cfg Config) *Provider {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("BASETEN_API_KEY")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	// TS createChatModel uses modelURL only for a /sync/v1 deployment; any
	// other URL (a /sync embeddings deployment) leaves chat on the Model APIs
	// at baseURL (a /predict URL is rejected by LanguageModel).
	chatBaseURL := baseURL
	customChat := strings.Contains(cfg.ModelURL, "/sync/v1")
	if customChat {
		chatBaseURL = cfg.ModelURL
	}
	// TS c4a8b80e02: transformBasetenRequestBody is wired only on the
	// default Model APIs chat path, not on a /sync/v1 custom deployment.
	var transform func(map[string]interface{}) (map[string]interface{}, []types.Warning)
	if !customChat {
		transform = transformBasetenRequestBody
	}

	structuredOutputs := true
	chatProvider := openai.New(openai.Config{
		APIKey:            apiKey,
		APIKeyEnvVar:      "BASETEN_API_KEY",
		APIKeyDescription: "Baseten API key",
		BaseURL:           chatBaseURL,
		Headers:           cfg.Headers,
		HTTPClient:        cfg.HTTPClient,
		Name:              "baseten",
		ChatProviderName:  "baseten.chat",
		// TS createChatModel builds an OpenAICompatibleChatLanguageModel,
		// which supports video_url content parts (7dd9ec320c).
		AllowVideo:                true,
		OpenAICompatible:          true,
		SupportsStructuredOutputs: &structuredOutputs, // TS supportsStructuredOutputs: true
		IncludeUsage:              true,
		UserAgentName:             "baseten",
		// TS basetenErrorStructure.
		CompatErrorStructure: basetenErrorStructure,

		TransformRequestBodyWithWarnings: transform,
	})

	p := &Provider{
		Provider: chatProvider,
		modelURL: cfg.ModelURL,
	}

	if cfg.ModelURL != "" && strings.Contains(cfg.ModelURL, "/sync") {
		p.embedding = openai.New(openai.Config{
			APIKey:            apiKey,
			APIKeyEnvVar:      "BASETEN_API_KEY",
			APIKeyDescription: "Baseten API key",
			BaseURL:           basetenEmbeddingURL(cfg.ModelURL),
			Headers:           cfg.Headers,
			HTTPClient:        cfg.HTTPClient,
			Name:              "baseten",
			OpenAICompatible:  true,
			IncludeUsage:      true,
			UserAgentName:     "baseten",
			// TS basetenErrorStructure.
			CompatErrorStructure: basetenErrorStructure,
		})
	}

	return p
}

// basetenDeepSeekV4Pro0813 is the Model APIs model whose JSON mode with
// reasoning returns the answer as reasoning and leaves content empty.
const basetenDeepSeekV4Pro0813 = "deepseek-ai/DeepSeek-V4-Pro-0813"

// transformBasetenRequestBody is TS transformBasetenRequestBody
// (baseten-provider.ts, c4a8b80e02): for DeepSeek V4 Pro 0813 with
// reasoning not "none" and a json_schema / json_object response_format, the
// response_format is dropped and a new leading system message carries the
// JSON instruction (injectJsonInstructionIntoMessages({messages: [],
// schema})), with a compatibility warning. Every other body is unchanged.
func transformBasetenRequestBody(body map[string]interface{}) (map[string]interface{}, []types.Warning) {
	if model, _ := body["model"].(string); model != basetenDeepSeekV4Pro0813 {
		return body, nil
	}
	if effort, ok := body["reasoning_effort"].(string); ok && effort == "none" {
		return body, nil
	}
	responseFormat, _ := body["response_format"].(map[string]interface{})
	formatType, _ := responseFormat["type"].(string)
	if formatType != "json_schema" && formatType != "json_object" {
		return body, nil
	}
	var schema interface{}
	if js, ok := responseFormat["json_schema"].(map[string]interface{}); ok {
		schema = js["schema"]
	}
	system := map[string]interface{}{
		"role":    "system",
		"content": providerutils.InjectJSONInstructionSchema("", schema, providerutils.InjectJSONInstructionOptions{}),
	}
	out := make(map[string]interface{}, len(body))
	for k, v := range body {
		if k != "response_format" {
			out[k] = v
		}
	}
	switch msgs := body["messages"].(type) {
	case []map[string]interface{}:
		out["messages"] = append([]map[string]interface{}{system}, msgs...)
	case []interface{}:
		out["messages"] = append([]interface{}{system}, msgs...)
	default:
		out["messages"] = []map[string]interface{}{system}
	}
	return out, []types.Warning{{
		Type:    "compatibility",
		Feature: "responseFormat",
		Details: "JSON instructions are injected into the system message and response_format is omitted because Baseten DeepSeek V4 Pro 0813 returns JSON as reasoning when JSON mode and reasoning are combined. The response is not constrained by the server.",
	}}
}

// basetenEmbeddingURL mirrors TS getCommonModelConfig's embedding URL
// handling: a /sync URL that isn't already /sync/v1 needs /v1 appended so the
// OpenAI-compatible embeddings path resolves correctly.
func basetenEmbeddingURL(modelURL string) string {
	if strings.Contains(modelURL, "/sync") && !strings.Contains(modelURL, "/sync/v1") {
		return modelURL + "/v1"
	}
	return modelURL
}

// LanguageModel returns an OpenAI-compatible Chat Completions model.
func (p *Provider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	// TS createChatModel: a /sync/v1 URL wins, then a /predict URL throws
	// (the TS Error text).
	if p.modelURL != "" && !strings.Contains(p.modelURL, "/sync/v1") && strings.Contains(p.modelURL, "/predict") {
		return nil, errors.New("Not supported. You must use a /sync/v1 endpoint for chat models.") //nolint:staticcheck // TS error text
	}
	if modelID == "" {
		// TS: modelId ?? 'placeholder' for a custom /sync/v1 deployment
		// (dedicated single-model endpoints ignore the "model" wire field),
		// modelId ?? 'chat' for the default Model APIs.
		if strings.Contains(p.modelURL, "/sync/v1") {
			modelID = "placeholder"
		} else {
			modelID = "chat"
		}
	}
	return p.Provider.ChatModel(modelID)
}

// ChatModel is an alias for LanguageModel (TS provider.chatModel).
func (p *Provider) ChatModel(modelID string) (provider.LanguageModel, error) {
	return p.LanguageModel(modelID)
}

// EmbeddingModel returns an embedding model. Baseten has no default
// embeddings endpoint; Config.ModelURL must be set to a /sync or /sync/v1
// deployment URL (TS: "No model URL provided for embeddings...").
func (p *Provider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	if p.embedding == nil {
		if p.modelURL == "" {
			return nil, errors.New("No model URL provided for embeddings. Please set modelURL option for embeddings.") //nolint:staticcheck // TS error text
		}
		return nil, errors.New("Not supported. You must use a /sync or /sync/v1 endpoint for embeddings.") //nolint:staticcheck // TS error text
	}
	if modelID == "" {
		modelID = "embeddings"
	}
	base, err := p.embedding.EmbeddingModel(modelID)
	if err != nil {
		return nil, err
	}
	openaiEmbedding, ok := base.(*openai.EmbeddingModel)
	if !ok {
		return base, nil
	}
	return &embeddingModel{EmbeddingModel: openaiEmbedding}, nil
}

// TextEmbeddingModel is an alias for EmbeddingModel (TS
// provider.textEmbeddingModel, deprecated in favor of embeddingModel).
func (p *Provider) TextEmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	return p.EmbeddingModel(modelID)
}

// embeddingModel overrides the inherited OpenAI-compatible embedding model's
// MaxEmbeddingsPerCall with Baseten's lower per-request batch limit.
type embeddingModel struct {
	*openai.EmbeddingModel
}

func (m *embeddingModel) MaxEmbeddingsPerCall() int {
	return maxEmbeddingsPerCall
}

// DoEmbedMany enforces Baseten's lower batch limit (TS maxEmbeddingsPerCall
// check in the OpenAI-compatible embedding model).
func (m *embeddingModel) DoEmbedMany(ctx context.Context, inputs []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
	if len(inputs) > maxEmbeddingsPerCall {
		return nil, &providerrors.TooManyEmbeddingValuesForCallError{
			Provider:             m.Provider(),
			ModelID:              m.ModelID(),
			MaxEmbeddingsPerCall: maxEmbeddingsPerCall,
			Values:               inputs,
		}
	}
	return m.EmbeddingModel.DoEmbedMany(ctx, inputs, opts)
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "baseten"
}

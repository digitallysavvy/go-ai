package azure

import (
	"context"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// LanguageModel implements the provider.LanguageModel interface for Azure
// OpenAI Chat Completions. TS azure.chat() is an OpenAIChatLanguageModel
// pointed at the deployment URL, so every request/response behavior
// (provider options, structured output, usage, annotations, reasoning
// models, file references) is delegated to the OpenAI chat model.
type LanguageModel struct {
	provider     *Provider
	deploymentID string

	once  sync.Once
	inner *openai.LanguageModel
}

// delegate lazily builds the OpenAI chat model configured for this Azure
// deployment (base URL, api-key header / token transport, api-version query).
func (m *LanguageModel) delegate() *openai.LanguageModel {
	m.once.Do(func() {
		p := m.provider
		var chatQuery map[string]string
		if p.includeAPIVersionQuery() {
			chatQuery = map[string]string{"api-version": p.config.APIVersion}
		}
		httpClient := p.httpClient
		if p.client != nil {
			httpClient = p.client.HTTPClient()
		}
		chatProvider := openai.New(openai.Config{
			Name:             "azure",
			BaseURL:          p.responsesBaseURL(m.deploymentID),
			Headers:          p.staticAuthHeaders(),
			ChatProviderName: "azure.chat",
			ChatQuery:        chatQuery,
			HTTPClient:       httpClient,
		})
		m.inner = openai.NewLanguageModel(chatProvider, m.deploymentID)
	})
	return m.inner
}

// NewLanguageModel creates a new Azure OpenAI language model
func NewLanguageModel(provider *Provider, deploymentID string) *LanguageModel {
	return &LanguageModel{
		provider:     provider,
		deploymentID: deploymentID,
	}
}

// SpecificationVersion returns the specification version
func (m *LanguageModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *LanguageModel) Provider() string {
	return "azure.chat"
}

// ModelID returns the deployment ID (Azure's equivalent of model ID)
func (m *LanguageModel) ModelID() string {
	return m.deploymentID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	return true
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return true
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	// Vision support depends on the deployed model
	return true
}

// DoGenerate performs non-streaming text generation.
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	return m.delegate().DoGenerate(ctx, opts)
}

// DoStream performs streaming text generation.
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	return m.delegate().DoStream(ctx, opts)
}

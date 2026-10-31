package googlevertex

import (
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
)

// LanguageModel wraps the shared Gemini language model implementation for the
// Google Vertex AI API. Authentication and base URL are set on the HTTP client
// by the Provider constructor; this type supplies the remaining Vertex-specific
// configuration (metadata key, provider options key precedence, etc.).
type LanguageModel struct {
	*gemini.LanguageModel
	provider *Provider
}

// NewLanguageModel creates a Google Vertex AI language model.
func NewLanguageModel(p *Provider, modelID string) *LanguageModel {
	return newLanguageModel(p, modelID, "google.vertex.chat")
}

// newLanguageModel is NewLanguageModel with the provider name its errors
// report; the image model passes its own (TS google-vertex-image-model.ts
// builds its GoogleLanguageModel with the image model's provider).
func newLanguageModel(p *Provider, modelID, providerName string) *LanguageModel {
	client := p.mainClient()
	if c, err := p.clients(); err == nil && isEndpointModelID(modelID) && c.endpointClient != nil {
		// Tuned models are served from ".../locations/{region}/endpoints/{id}",
		// which omits the "/publishers/google" suffix the base-model paths
		// carry (TS isEndpointModelId / loadBaseURL({endpoint: true})).
		client = c.endpointClient
	}
	cfg := gemini.Config{
		ProviderName: providerName,
		MetadataKey:  "vertex",
		// TS parity: Vertex reads "googleVertex" first, then the legacy "vertex"
		// key, then "google" (cross-namespace fallback), and writes provider
		// metadata under both "googleVertex" and "vertex".
		ProviderOptionsKeys: []string{"googleVertex", "vertex", "google"},
		MetadataKeys:        []string{"googleVertex", "vertex"},
		IsVertex:            true,
		GeneratePath: func(id string) string {
			return fmt.Sprintf("/%s:generateContent", gemini.GetModelPath(id))
		},
		StreamPath: func(id string) string {
			return fmt.Sprintf("/%s:streamGenerateContent?alt=sse", gemini.GetModelPath(id))
		},
		Client:             client,
		GenerateID:         p.config.GenerateID,
		SupportsImageInput: vertexSupportsImageInput,
		SupportedURLs: func(string) map[string][]string {
			return map[string][]string{
				"*": {`^https?:\/\/.*$`, `^gs:\/\/.*$`},
			}
		},
		ToolResultDownloadMaxBytes:     vertexToolResultDownloadMaxBytes(p.config.ToolResultDownloads),
		SupportsGoogleCloudStorageUrls: true,
	}
	return &LanguageModel{LanguageModel: gemini.NewLanguageModel(cfg, modelID), provider: p}
}

// vertexToolResultDownloadMaxBytes returns the configured max download size,
// defaulting to gemini.DefaultToolResultDownloadMaxBytes (7 MiB) when unset,
// matching TS toolResultDownloads.maxBytes default.
func vertexToolResultDownloadMaxBytes(cfg ToolResultDownloadsConfig) int64 {
	if cfg.MaxBytes > 0 {
		return cfg.MaxBytes
	}
	return gemini.DefaultToolResultDownloadMaxBytes
}

// vertexSupportsImageInput reports whether a Vertex AI model accepts image inputs.
func vertexSupportsImageInput(modelID string) bool {
	switch modelID {
	case "gemini-pro-vision", "gemini-1.5-pro", "gemini-1.5-flash",
		"gemini-1.5-flash-8b", "gemini-2.0-flash-exp":
		return true
	}
	return strings.HasPrefix(modelID, "gemini-2.") ||
		strings.HasPrefix(modelID, "gemini-3.")
}

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
	cfg := gemini.Config{
		ProviderName: "google-vertex",
		MetadataKey:  "vertex",
		// TS parity: Vertex reads "googleVertex" first, then the legacy "vertex"
		// key, then "google" (cross-namespace fallback), and writes provider
		// metadata under both "googleVertex" and "vertex".
		ProviderOptionsKeys: []string{"googleVertex", "vertex", "google"},
		MetadataKeys:        []string{"googleVertex", "vertex"},
		IsVertex:            true,
		GeneratePath: func(id string) string {
			return fmt.Sprintf("/models/%s:generateContent", id)
		},
		StreamPath: func(id string) string {
			return fmt.Sprintf("/models/%s:streamGenerateContent?alt=sse", id)
		},
		Client:             p.client,
		SupportsImageInput: vertexSupportsImageInput,
		SupportedURLs: func(string) map[string][]string {
			return map[string][]string{
				"*": {`^https?:\/\/.*$`, `^gs:\/\/.*$`},
			}
		},
	}
	return &LanguageModel{LanguageModel: gemini.NewLanguageModel(cfg, modelID), provider: p}
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

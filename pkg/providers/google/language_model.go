package google

import (
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
)

// LanguageModel wraps the shared Gemini language model implementation for the
// Google Generative AI API. All wire format logic lives in the gemini package;
// this type exists to give callers a named google.LanguageModel type and to
// supply the provider-specific configuration (auth path, metadata key, etc.).
type LanguageModel struct {
	*gemini.LanguageModel
	provider *Provider
}

// NewLanguageModel creates a Google Generative AI language model.
func NewLanguageModel(p *Provider, modelID string) *LanguageModel {
	cfg := gemini.Config{
		ProviderName:        p.Name(),
		MetadataKey:         "google",
		ProviderOptionsKeys: []string{"google"},
		GeneratePath: func(id string) string {
			return fmt.Sprintf("/models/%s:generateContent", id)
		},
		StreamPath: func(id string) string {
			return fmt.Sprintf("/models/%s:streamGenerateContent?alt=sse", id)
		},
		Client:                p.client,
		SupportsCodeExecution: true,
		SupportsImageInput:    googleSupportsImageInput,
	}
	return &LanguageModel{LanguageModel: gemini.NewLanguageModel(cfg, modelID), provider: p}
}

// googleSupportsImageInput reports whether a Google Generative AI model accepts
// image inputs. This list covers the models that explicitly support vision.
func googleSupportsImageInput(modelID string) bool {
	switch modelID {
	case "gemini-pro-vision", "gemini-1.5-pro", "gemini-1.5-flash":
		return true
	}
	// Gemini 2.x and newer models generally support image input.
	return strings.HasPrefix(modelID, "gemini-2.") ||
		strings.HasPrefix(modelID, "gemini-3.")
}

package google

import (
	"fmt"
	"regexp"
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
		Client:             p.client,
		SupportsImageInput: googleSupportsImageInput,
		SupportedURLs:      func(modelID string) map[string][]string { return googleSupportedURLs(p.config.BaseURL, modelID) },
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

// supportedExternalURLMediaTypes mirrors TS google-provider.ts
// supportedExternalUrlMediaTypes: media types accepted as external https
// URLs directly (without downloading), for models that support it.
var supportedExternalURLMediaTypes = []string{
	"text/html",
	"text/css",
	"text/plain",
	"text/xml",
	"text/csv",
	"text/rtf",
	"text/javascript",
	"application/json",
	"application/pdf",
	"image/bmp",
	"image/jpeg",
	"image/png",
	"image/webp",
	"video/mp4",
	"video/mpeg",
	"video/quicktime",
	"video/avi",
	"video/x-flv",
	"video/mpg",
	"video/webm",
	"video/wmv",
	"video/3gpp",
}

var (
	googleGemini2_0Pattern = regexp.MustCompile(`(^|/)gemini-2\.0`)
	googleGeminiPattern    = regexp.MustCompile(`(^|/)gemini-`)
)

// googleSupportsExternalFileUrls mirrors TS supportsExternalFileUrls: any
// Gemini model except the 2.0 family accepts external https URLs directly.
func googleSupportsExternalFileUrls(modelID string) bool {
	return googleGeminiPattern.MatchString(modelID) && !googleGemini2_0Pattern.MatchString(modelID)
}

// googleSupportedURLs mirrors TS google-provider.ts getSupportedUrls: the
// Files API URL pattern and two YouTube patterns are always supported; for
// non-2.0 Gemini models, every entry in supportedExternalURLMediaTypes also
// accepts any https URL directly.
func googleSupportedURLs(baseURL, modelID string) map[string][]string {
	out := map[string][]string{
		"*": {
			`^https:\/\/generativelanguage\.googleapis\.com\/v1beta\/files\/.*$`,
			"^" + regexp.QuoteMeta(baseURL) + `/files/.*$`,
			`^https:\/\/(?:www\.)?youtube\.com\/watch\?v=[\w-]+(?:&[\w=&.-]*)?$`,
			`^https:\/\/youtu\.be\/[\w-]+(?:\?[\w=&.-]*)?$`,
		},
	}
	if googleSupportsExternalFileUrls(modelID) {
		for _, mediaType := range supportedExternalURLMediaTypes {
			out[mediaType] = []string{`^https:\/\/.*$`}
		}
	}
	return out
}

package gemini

import (
	"context"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
)

// Config parameterizes the shared Gemini language model implementation
// for both the google and googlevertex providers.
//
// The two providers share identical wire formats, request building,
// response parsing, and streaming logic. Only authentication, base URL,
// and a handful of runtime strings differ — those are captured here and
// injected at construction time, matching the pattern used by the TS SDK
// where GoogleGenerativeAILanguageModel is shared between both packages.
type Config struct {
	// ProviderName is returned by LanguageModel.Provider() and used in error
	// wrapping. "google" for Google Generative AI; "google-vertex" for Vertex AI.
	ProviderName string

	// MetadataKey is the top-level key used in ProviderMetadata output maps.
	// "google" for Google Generative AI; "vertex" for Vertex AI.
	MetadataKey string

	// ProviderOptionsKeys is the ordered list of keys checked when reading
	// caller-supplied provider options from GenerateOptions.ProviderOptions.
	// Google uses ["google"]; Vertex uses ["vertex", "googleVertex", "google"].
	ProviderOptionsKeys []string

	// GeneratePath returns the full HTTP path for a non-streaming request.
	GeneratePath func(modelID string) string

	// StreamPath returns the full HTTP path for a streaming request.
	StreamPath func(modelID string) string

	// Client is the pre-configured HTTP client with auth headers already set.
	Client *internalhttp.Client

	// SupportsImageInput returns whether a given model ID accepts image inputs.
	// When nil, the method returns false.
	SupportsImageInput func(modelID string) bool

	// IsVertex marks the Vertex AI provider (TS: provider starts with
	// "google.vertex."). When false, ProviderName == "google-vertex" is also
	// treated as Vertex for backward compatibility.
	IsVertex bool

	// MetadataKeys are the keys ProviderMetadata payloads are written under.
	// Defaults to []string{MetadataKey}. Vertex writes under both
	// "googleVertex" and "vertex" (TS wrapProviderMetadata).
	MetadataKeys []string

	// SupportedURLs returns the URL patterns (regular expressions keyed by
	// media type, "*" for all) the model accepts directly.
	SupportedURLs func(modelID string) map[string][]string

	// ToolResultDownloadMaxBytes enables downloading http(s) file URLs in tool
	// results to inline data before conversion (Vertex only accepts inline
	// data in function responses). Zero disables downloading.
	ToolResultDownloadMaxBytes int64

	// ToolResultDownload overrides the downloader used for tool-result files
	// (tests). Returns the bytes and the response content type.
	ToolResultDownload func(ctx context.Context, url string, maxBytes int64) ([]byte, string, error)

	// GenerateID generates IDs for tool calls and sources. Defaults to a
	// random ID generator.
	GenerateID func() string
}

// metadataKeys returns the keys ProviderMetadata payloads are written under,
// defaulting to []string{MetadataKey} when MetadataKeys is unset.
func (c Config) metadataKeys() []string {
	if len(c.MetadataKeys) > 0 {
		return c.MetadataKeys
	}
	return []string{c.MetadataKey}
}

// wrapProviderMetadata returns payload under every configured metadata key
// (TS wrapProviderMetadata: Object.fromEntries(providerOptionsNames.map(name
// => [name, payload]))). Vertex writes the same payload under both
// "googleVertex" and "vertex".
func (c Config) wrapProviderMetadata(payload interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for _, key := range c.metadataKeys() {
		out[key] = payload
	}
	return out
}

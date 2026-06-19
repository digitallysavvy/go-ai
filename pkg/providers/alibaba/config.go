package alibaba

import (
	"fmt"
	"os"
)

// Config contains configuration for the Alibaba Cloud (DashScope) provider
type Config struct {
	// APIKey is the Alibaba Cloud API key
	// Can be obtained from https://dashscope.console.aliyun.com/
	APIKey string

	// BaseURL is the base URL for the chat API (optional)
	// Default: https://dashscope-intl.aliyuncs.com/compatible-mode/v1
	// This is the OpenAI-compatible endpoint for Qwen models
	BaseURL string

	// VideoBaseURL is the base URL for the video API (optional)
	// Default: https://dashscope-intl.aliyuncs.com
	// This is the DashScope native endpoint for Wan video models
	VideoBaseURL string

	// EmbeddingBaseURL is the base URL for the embedding API (optional)
	// Default: https://dashscope-intl.aliyuncs.com/api/v1
	// This is the DashScope native endpoint for embedding models
	EmbeddingBaseURL string

	// Headers are custom HTTP headers to include in requests.
	Headers map[string]string `json:"headers,omitempty"`
}

// NewConfig creates a new Alibaba Cloud provider configuration
// If apiKey is empty, it attempts to load from the ALIBABA_API_KEY environment variable
func NewConfig(apiKey string) (Config, error) {
	if apiKey == "" {
		apiKey = os.Getenv("ALIBABA_API_KEY")
	}

	if apiKey == "" {
		return Config{}, fmt.Errorf("LAlibaba API key is required. Set ALIBABA_API_KEY environment variable or provide it in Config")
	}

	return Config{
		APIKey: apiKey,
	}, nil
}

// Validate checks if the configuration is valid
func (c Config) Validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("LAPI key is required")
	}
	return nil
}

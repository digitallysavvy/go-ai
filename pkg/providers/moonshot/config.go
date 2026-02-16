package moonshot

import (
	"fmt"
	"os"
)

// Config contains configuration for the Moonshot AI provider
type Config struct {
	// APIKey is the Moonshot API key
	// Can be obtained from https://platform.moonshot.cn/
	APIKey string

	// BaseURL is the base URL for the API (optional)
	// Default: https://api.moonshot.cn/v1
	BaseURL string
}

// NewConfig creates a new Moonshot provider configuration
// If apiKey is empty, it attempts to load from the MOONSHOT_API_KEY environment variable
func NewConfig(apiKey string) (Config, error) {
	if apiKey == "" {
		apiKey = os.Getenv("MOONSHOT_API_KEY")
	}

	if apiKey == "" {
		return Config{}, fmt.Errorf("Moonshot API key is required. Set MOONSHOT_API_KEY environment variable or provide it in Config")
	}

	return Config{
		APIKey: apiKey,
	}, nil
}

// Validate checks if the configuration is valid
func (c Config) Validate() error {
	if c.APIKey == "" {
		return fmt.Errorf("API key is required")
	}
	return nil
}

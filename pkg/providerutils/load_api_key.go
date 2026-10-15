package providerutils

import (
	"os"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// APIKeyOrEnv returns apiKey, or the value of the envVar environment variable
// when apiKey is empty (the lookup half of TS loadApiKey).
func APIKeyOrEnv(apiKey, envVar string) string {
	if apiKey != "" {
		return apiKey
	}
	return os.Getenv(envVar)
}

// LoadAPIKey mirrors TS loadApiKey: the explicit apiKey, else the envVar
// environment variable, else a *providererrors.LoadAPIKeyError naming the
// provider description and the variable.
func LoadAPIKey(apiKey, envVar, description string) (string, error) {
	if key := APIKeyOrEnv(apiKey, envVar); key != "" {
		return key, nil
	}
	return "", providererrors.NewLoadAPIKeyError(description, envVar)
}

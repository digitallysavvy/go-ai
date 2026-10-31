package providerutils

import (
	"os"
	"strings"

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

// RequestAuthHeader resolves one provider credential header at request time
// like a TS `headers: () => ({ [header]: prefix + loadApiKey(...) })` option:
// the key comes from apiKey or the envVar environment variable (read per
// request) and a missing key is a *providererrors.LoadAPIKeyError.
//
// A caller that supplies the credential header itself through userHeaders
// (case-insensitive, or any name in alsoHeaders) keeps its own credential and
// gets (nil, nil), so existing header-authenticated setups keep working.
// The result is meant for http.Config.HeadersFunc.
func RequestAuthHeader(userHeaders map[string]string, apiKey, envVar, description, header, prefix string, alsoHeaders ...string) (map[string]string, error) {
	for k := range userHeaders {
		if strings.EqualFold(k, header) {
			return nil, nil
		}
		for _, h := range alsoHeaders {
			if strings.EqualFold(k, h) {
				return nil, nil
			}
		}
	}
	key, err := LoadAPIKey(apiKey, envVar, description)
	if err != nil {
		return nil, err
	}
	return map[string]string{header: prefix + key}, nil
}

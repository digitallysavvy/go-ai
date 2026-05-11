package providerutils

import (
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ResolveProviderReference returns the provider-specific file identifier from
// a provider reference map.
func ResolveProviderReference(reference types.ProviderReference, provider string) (string, error) {
	if id, ok := reference[provider]; ok {
		return id, nil
	}
	return "", &providererrors.NoSuchProviderReferenceError{
		Provider:  provider,
		Reference: reference,
	}
}

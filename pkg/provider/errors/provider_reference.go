package errors

import (
	"fmt"
	"sort"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// NoSuchProviderReferenceError is returned when a provider reference does not
// contain an identifier for the requested provider.
type NoSuchProviderReferenceError struct {
	Provider  string
	Reference types.ProviderReference
}

func (e *NoSuchProviderReferenceError) Error() string {
	return fmt.Sprintf(
		"No provider reference found for provider '%s'. Available providers: %s",
		e.Provider,
		strings.Join(providerReferenceKeys(e.Reference), ", "),
	)
}

func providerReferenceKeys(reference types.ProviderReference) []string {
	keys := make([]string, 0, len(reference))
	for key := range reference {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

package bedrock

import (
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// normalizeCallOptions returns opts with its provider options read through
// their JSON form (providerutils.NormalizeOptionsJSON), so Go-typed literals
// (map[string]string, []map[string]interface{}, typed option structs, int64)
// reach the map-based option readers and validators in the shape TS
// parseProviderOptions sees: JSON values. opts itself is not modified.
func normalizeCallOptions(opts *provider.GenerateOptions) *provider.GenerateOptions {
	if opts == nil || len(opts.ProviderOptions) == 0 {
		return opts
	}
	normalized := *opts
	normalized.ProviderOptions = providerutils.NormalizeOptionsJSON(opts.ProviderOptions)
	return &normalized
}

// normalizedNamespace returns providerOptions[name] read through its JSON
// form (a part or message provider-options namespace given as a Go-typed
// literal); present is false when the key is absent or null.
func normalizedNamespace(providerOptions map[string]interface{}, name string) (value interface{}, present bool) {
	raw, ok := providerOptions[name]
	if !ok || raw == nil {
		return nil, false
	}
	return providerutils.NormalizeOptionsJSON(map[string]interface{}{name: raw})[name], true
}

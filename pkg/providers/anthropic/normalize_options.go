package anthropic

import (
	"reflect"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// normalizeCallOptions returns opts with its provider options read through
// their JSON form (providerutils.NormalizeOptionsJSON), so Go-typed literals
// (map[string]string, []map[string]interface{}, typed option structs, int64)
// reach the map-based option readers in the shape TS parseProviderOptions
// sees: JSON values. opts itself is not modified.
func normalizeCallOptions(opts *provider.GenerateOptions) *provider.GenerateOptions {
	if opts == nil || len(opts.ProviderOptions) == 0 {
		return opts
	}
	normalized := *opts
	normalized.ProviderOptions = providerutils.NormalizeOptionsJSON(opts.ProviderOptions)
	return &normalized
}

// normalizedOptionsMap returns a provider-options value of any Go map type
// (map[string]interface{}, map[string]map[string]bool, ...) as a JSON-shaped
// map[string]interface{}; ok is false for nil or a non-map value.
func normalizedOptionsMap(v interface{}) (map[string]interface{}, bool) {
	switch m := v.(type) {
	case nil:
		return nil, false
	case map[string]interface{}:
		return providerutils.NormalizeOptionsJSON(m), true
	}
	if reflect.ValueOf(v).Kind() != reflect.Map {
		return nil, false
	}
	out, ok := providerutils.NormalizeOptionsJSON(map[string]interface{}{"v": v})["v"].(map[string]interface{})
	return out, ok
}

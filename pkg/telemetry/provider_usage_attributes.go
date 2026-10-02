package telemetry

import (
	"math"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel/attribute"
)

// usageKeyAliases ports TS's otel/src/provider-usage-attributes.ts alias
// table: common provider usage field spellings are normalized to a shared
// set of attribute suffixes so observability backends see consistent names
// regardless of which provider reported the usage.
var usageKeyAliases = map[string]string{
	"inputtokens":          "input_tokens",
	"prompttokens":         "input_tokens",
	"prompttokencount":     "input_tokens",
	"totalinputtokens":     "input_tokens",
	"outputtokens":         "output_tokens",
	"completiontokens":     "output_tokens",
	"completiontokencount": "output_tokens",
	"candidatestokencount": "output_tokens",
	"totaltokens":          "total_tokens",
	"totaltokencount":      "total_tokens",
}

var usageCamelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
var usageNonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]+`)
var usageAliasStrip = regexp.MustCompile(`[^a-zA-Z0-9]`)

// normalizeUsageKey mirrors TS's normalizeUsageKey: alias lookup first (on a
// lowercased, non-alphanumeric-stripped key), then a generic
// camelCase/kebab-case/snake-ish transform to lower_snake_case.
func normalizeUsageKey(key string) string {
	aliasKey := strings.ToLower(usageAliasStrip.ReplaceAllString(key, ""))
	if alias, ok := usageKeyAliases[aliasKey]; ok {
		return alias
	}

	transformed := usageCamelBoundary.ReplaceAllString(key, "${1}_${2}")
	transformed = usageNonAlnum.ReplaceAllString(transformed, "_")
	transformed = strings.Trim(transformed, "_")
	return strings.ToLower(transformed)
}

// usageNumericValue converts common numeric JSON representations (float64
// from encoding/json, plain int/int64 from hand-built provider maps) to a
// float64, mirroring TS's `typeof value === 'number'` check.
func usageNumericValue(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

// getProviderUsageAttributes converts a provider-native usage object into
// numeric span attributes under prefix, recursing into nested objects and
// normalizing each leaf key. Mirrors TS's getProviderUsageAttributes
// (otel/src/provider-usage-attributes.ts). Non-finite numbers and
// non-numeric/non-object values are skipped, matching TS's
// `Number.isFinite(value) && path.length > 0` guard.
func getProviderUsageAttributes(usage map[string]interface{}, prefix string) []attribute.KeyValue {
	if len(usage) == 0 {
		return nil
	}
	var attrs []attribute.KeyValue
	var addValue func(v interface{}, path []string)
	addValue = func(v interface{}, path []string) {
		if n, ok := usageNumericValue(v); ok {
			if len(path) == 0 || math.IsNaN(n) || math.IsInf(n, 0) {
				return
			}
			attrs = append(attrs, attribute.Float64(prefix+"."+strings.Join(path, "."), n))
			return
		}
		nested, ok := v.(map[string]interface{})
		if !ok {
			return
		}
		for key, nestedValue := range nested {
			normalizedKey := normalizeUsageKey(key)
			if normalizedKey != "" {
				addValue(nestedValue, append(append([]string{}, path...), normalizedKey))
			}
		}
	}
	addValue(usage, nil)
	return attrs
}

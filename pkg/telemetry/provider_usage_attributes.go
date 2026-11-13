package telemetry

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/jscase"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
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
	return jscase.ToLower(transformed)
}

// usageNumericValue converts common numeric JSON representations (float64
// from encoding/json, json.Number from a UseNumber or ordered decode, plain
// Go integers from hand-built provider maps) to a float64, mirroring TS's
// `typeof value === 'number'` check.
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
	case int16:
		return float64(n), true
	case int8:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint8:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			// A json.Number that does not parse is not a JSON number; a
			// value out of float64 range parses to +/-Inf (skipped below).
			if math.IsInf(f, 0) {
				return f, true
			}
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// usageEntries returns an object's entries in the order a JS Object.entries
// call would visit them. An ordered object (*jsonorder.Object, from an
// ordered decode) keeps its wire order. A Go map has no order, so its keys
// are visited in the order Go serializes the same map in ai.response.usage
// (sorted, with array-index keys first as JS enumerates them): when two keys
// normalize to the same attribute, the later of them in that order wins,
// like the later key wins in TS.
func usageEntries(v interface{}) ([]string, func(string) interface{}, bool) {
	switch obj := v.(type) {
	case map[string]interface{}:
		ordered := jsonorder.FromMap(obj)
		return ordered.Keys(), func(k string) interface{} { return obj[k] }, true
	case *jsonorder.Object:
		if obj == nil {
			return nil, nil, false
		}
		return obj.Keys(), func(k string) interface{} {
			val, _ := obj.Get(k)
			return val
		}, true
	default:
		return nil, nil, false
	}
}

// getProviderUsageAttributes converts a provider-native usage object into
// numeric span attributes under prefix, recursing into nested objects and
// normalizing each leaf key. Mirrors TS's getProviderUsageAttributes
// (otel/src/provider-usage-attributes.ts). Non-finite numbers and
// non-numeric/non-object values are skipped, matching TS's
// `Number.isFinite(value) && path.length > 0` guard.
//
// TS writes into one attributes object, so two keys that normalize to the
// same attribute (inputTokens and input_tokens) yield one attribute: the
// later key's value, at the position the first one took. Go returns the same
// single attribute per name, visiting keys in usageEntries order.
func getProviderUsageAttributes(usage map[string]interface{}, prefix string) []attribute.KeyValue {
	if len(usage) == 0 {
		return nil
	}
	var attrs []attribute.KeyValue
	index := map[string]int{}
	var addValue func(v interface{}, path []string)
	addValue = func(v interface{}, path []string) {
		if n, ok := usageNumericValue(v); ok {
			if len(path) == 0 || math.IsNaN(n) || math.IsInf(n, 0) {
				return
			}
			name := prefix + "." + strings.Join(path, ".")
			if i, seen := index[name]; seen {
				attrs[i] = jsNumberAttr(name, n)
				return
			}
			index[name] = len(attrs)
			attrs = append(attrs, jsNumberAttr(name, n))
			return
		}
		keys, get, ok := usageEntries(v)
		if !ok {
			return
		}
		for _, key := range keys {
			normalizedKey := normalizeUsageKey(key)
			if normalizedKey != "" {
				addValue(get(key), append(append([]string{}, path...), normalizedKey))
			}
		}
	}
	addValue(usage, nil)
	return attrs
}

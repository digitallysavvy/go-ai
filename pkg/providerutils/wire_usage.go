package providerutils

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ApplyWireUsageRaw sets result.Usage.Raw to the response body's `usage`
// object exactly as the API sent it. TS keeps the provider's usage payload
// verbatim in usage.raw (including fields the SDK has no schema for, such as
// cost_in_usd_ticks), while Go converters rebuild it from typed fields.
func ApplyWireUsageRaw(result *types.GenerateResult, body []byte) {
	if result == nil || len(body) == 0 {
		return
	}
	var env struct {
		Usage map[string]interface{} `json:"usage"`
	}
	if json.Unmarshal(body, &env) != nil || env.Usage == nil {
		return
	}
	result.Usage.Raw = env.Usage
}

// UsageRawSchema lists the usage fields a TS zod schema declares: zod strips
// everything else from usage.raw. Nested maps describe object-valued fields.
type UsageRawSchema struct {
	Keys   []string
	Nested map[string][]string
}

// ApplyWireUsageRawSchema is ApplyWireUsageRaw restricted to the fields in
// schema (null and absent values are kept as sent).
func ApplyWireUsageRawSchema(result *types.GenerateResult, body []byte, schema UsageRawSchema) {
	if result == nil {
		return
	}
	ApplyWireUsageRaw(result, body)
	raw := result.Usage.Raw
	if raw == nil {
		return
	}
	out := map[string]interface{}{}
	for _, k := range schema.Keys {
		if v, ok := raw[k]; ok {
			out[k] = v
		}
	}
	for k, subKeys := range schema.Nested {
		v, ok := raw[k]
		if !ok {
			continue
		}
		sub, isMap := v.(map[string]interface{})
		if !isMap {
			out[k] = v
			continue
		}
		f := map[string]interface{}{}
		for _, sk := range subKeys {
			if sv, ok := sub[sk]; ok {
				f[sk] = sv
			}
		}
		out[k] = f
	}
	result.Usage.Raw = out
}

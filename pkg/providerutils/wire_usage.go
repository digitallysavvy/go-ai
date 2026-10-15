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

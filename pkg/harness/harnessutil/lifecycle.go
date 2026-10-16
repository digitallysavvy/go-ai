package harnessutil

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// ValidateLifecycleStateObject validates adapter lifecycle `data` the way the
// TS adapters' zod `lifecycleStateSchema` does: the data must be an object
// whose fields have the types dst declares, and live bridge coordinates (a
// `bridge` object) must carry the required `port`, `token` and
// `lastSeenEventId`. Empty data and JSON null are accepted. Unknown keys are
// kept (loose objects), so dst only needs the known fields.
func ValidateLifecycleStateObject(data json.RawMessage, dst any) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &probe); err != nil {
		return fmt.Errorf("lifecycle state data must be an object: %w", err)
	}
	if err := json.Unmarshal(trimmed, dst); err != nil {
		return fmt.Errorf("invalid lifecycle state data: %w", err)
	}
	if raw, ok := probe["bridge"]; ok && string(bytes.TrimSpace(raw)) != "null" {
		var coords map[string]json.RawMessage
		if err := json.Unmarshal(raw, &coords); err != nil {
			return fmt.Errorf("invalid lifecycle state data: bridge must be an object: %w", err)
		}
		for _, key := range []string{"port", "token", "lastSeenEventId"} {
			if v, ok := coords[key]; !ok || string(bytes.TrimSpace(v)) == "null" {
				return fmt.Errorf("invalid lifecycle state data: bridge.%s is required", key)
			}
		}
	}
	return nil
}

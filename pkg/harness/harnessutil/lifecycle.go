package harnessutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
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
	// zod's `.optional()` accepts a missing key but rejects an explicit null,
	// so a declared field carrying null is a schema failure, not "unset".
	for _, key := range jsonFieldNames(dst) {
		if raw, ok := probe[key]; ok && string(bytes.TrimSpace(raw)) == "null" {
			return fmt.Errorf("invalid lifecycle state data: %s must not be null", key)
		}
	}
	if raw, ok := probe["bridge"]; ok {
		var coords map[string]json.RawMessage
		if err := json.Unmarshal(raw, &coords); err != nil {
			return fmt.Errorf("invalid lifecycle state data: bridge must be an object: %w", err)
		}
		for _, key := range []string{"port", "token", "lastSeenEventId"} {
			if v, ok := coords[key]; !ok || string(bytes.TrimSpace(v)) == "null" {
				return fmt.Errorf("invalid lifecycle state data: bridge.%s is required", key)
			}
		}
		// Only sandboxId is declared by these adapters' bridge schemas; other
		// keys (ACP's stateDir has its own validator) are stripped by z.object.
		if v, ok := coords["sandboxId"]; ok && string(bytes.TrimSpace(v)) == "null" {
			return fmt.Errorf("invalid lifecycle state data: bridge.sandboxId must not be null")
		}
	}
	return nil
}

// jsonFieldNames lists the JSON keys of the struct dst points to.
func jsonFieldNames(dst any) []string {
	t := reflect.TypeOf(dst)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	var names []string
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			names = append(names, name)
		}
	}
	return names
}

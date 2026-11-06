package acp

import (
	"encoding/json"
	"fmt"
)

// The helpers below deep-check the elements of a persisted turnStartConfig /
// coldSession the way acpTurnStartConfigSchema does: `tools` entries
// (acpSerializableToolSpecSchema), `builtinTools` entries (builtinToolSchema)
// and the optional mapping, model, debug, responseFormat and
// outputSchemaMapping members.

type rawObject = map[string]json.RawMessage

func asRawObject(raw json.RawMessage) (rawObject, bool) {
	if isJSONNull(raw) {
		return nil, false
	}
	var o rawObject
	if err := json.Unmarshal(raw, &o); err != nil || o == nil {
		return nil, false
	}
	return o, true
}

func isRawString(raw json.RawMessage) bool {
	var s string
	return !isJSONNull(raw) && json.Unmarshal(raw, &s) == nil
}

func isRawBool(raw json.RawMessage) bool {
	var b bool
	return !isJSONNull(raw) && json.Unmarshal(raw, &b) == nil
}

func nonEmptyRawString(raw json.RawMessage) bool {
	var s string
	return json.Unmarshal(raw, &s) == nil && s != ""
}

// optionalOK is zod `.optional()`: absent passes, an explicit null does not.
func optionalOK(o rawObject, key string, valid func(json.RawMessage) bool) bool {
	raw, ok := o[key]
	return !ok || valid(raw)
}

func enumRaw(allowed ...string) func(json.RawMessage) bool {
	return func(raw json.RawMessage) bool {
		var s string
		if json.Unmarshal(raw, &s) != nil || isJSONNull(raw) {
			return false
		}
		for _, a := range allowed {
			if s == a {
				return true
			}
		}
		return false
	}
}

func validateToolSpecs(items []json.RawMessage, key string) error {
	for i, item := range items {
		o, ok := asRawObject(item)
		if !ok {
			return fmt.Errorf("ACP lifecycle state data: %s.tools[%d] must be an object.", key, i) //nolint:staticcheck // mirrors the TS zod object
		}
		if raw, present := o["name"]; !present || !isRawString(raw) {
			return fmt.Errorf("ACP lifecycle state data: %s.tools[%d].name must be a string.", key, i) //nolint:staticcheck // mirrors the TS zod string
		}
		if !optionalOK(o, "description", isRawString) {
			return fmt.Errorf("ACP lifecycle state data: %s.tools[%d].description must be a string.", key, i) //nolint:staticcheck // mirrors the TS zod optional string
		}
		// z.custom: a present inputSchema must be a non-null, non-array JSON object.
		if !optionalOK(o, "inputSchema", func(raw json.RawMessage) bool { _, ok := asRawObject(raw); return ok }) {
			return fmt.Errorf("ACP lifecycle state data: %s.tools[%d].inputSchema must be a JSON object.", key, i) //nolint:staticcheck // mirrors the TS zod custom
		}
	}
	return nil
}

func validateBuiltinTools(items []json.RawMessage, key string) error {
	for i, item := range items {
		o, ok := asRawObject(item)
		if !ok {
			return fmt.Errorf("ACP lifecycle state data: %s.builtinTools[%d] must be an object.", key, i) //nolint:staticcheck // mirrors the TS zod object
		}
		if raw, present := o["toolName"]; !present || !isRawString(raw) {
			return fmt.Errorf("ACP lifecycle state data: %s.builtinTools[%d].toolName must be a string.", key, i) //nolint:staticcheck // mirrors the TS zod string
		}
		for _, name := range []string{"nativeName", "title"} {
			if !optionalOK(o, name, isRawString) {
				return fmt.Errorf("ACP lifecycle state data: %s.builtinTools[%d].%s must be a string.", key, i, name) //nolint:staticcheck // mirrors the TS zod optional string
			}
		}
		if !optionalOK(o, "toolUseKind", enumRaw("readonly", "edit", "bash")) {
			return fmt.Errorf("ACP lifecycle state data: %s.builtinTools[%d].toolUseKind must be one of readonly, edit, bash.", key, i) //nolint:staticcheck // mirrors the TS zod enum
		}
		// inputSchema is z.json().optional(): any JSON value (null included) passes.
	}
	return nil
}

func validatePermissionModeMapping(raw json.RawMessage, key string) error {
	invalid := fmt.Errorf("ACP lifecycle state data: %s.permissionModeMapping is invalid.", key) //nolint:staticcheck // mirrors the TS zod object
	o, ok := asRawObject(raw)
	if !ok {
		return invalid
	}
	for _, mode := range []string{"allow-reads", "allow-edits", "allow-all"} {
		target, present := o[mode]
		if !present {
			return invalid
		}
		if isJSONNull(target) {
			continue
		}
		t, ok := asRawObject(target)
		if !ok {
			return invalid
		}
		var typ string
		if r, present := t["type"]; !present || json.Unmarshal(r, &typ) != nil {
			return invalid
		}
		switch typ {
		case "session-mode":
			if r, present := t["modeId"]; !present || !isRawString(r) {
				return invalid
			}
		case "session-config-option":
			if r, present := t["configId"]; !present || !isRawString(r) {
				return invalid
			}
			if r, present := t["value"]; !present || (!isRawString(r) && !isRawBool(r)) {
				return invalid
			}
		default:
			return invalid
		}
	}
	return nil
}

func validateModelMapping(raw json.RawMessage, key string) error {
	invalid := fmt.Errorf("ACP lifecycle state data: %s.modelMapping is invalid.", key) //nolint:staticcheck // mirrors the TS zod discriminated union
	o, ok := asRawObject(raw)
	if !ok {
		return invalid
	}
	if r, present := o["type"]; !present || !enumRaw("session-config-option", "session-model")(r) {
		return invalid
	}
	if r, present := o["path"]; !present || !nonEmptyRawString(r) {
		return invalid
	}
	return nil
}

func validateOutputSchemaMapping(raw json.RawMessage, key string) error {
	invalid := fmt.Errorf("ACP lifecycle state data: %s.outputSchemaMapping is invalid.", key) //nolint:staticcheck // mirrors the TS zod object
	o, ok := asRawObject(raw)
	if !ok {
		return invalid
	}
	if r, present := o["type"]; !present || !enumRaw("session-prompt-meta")(r) {
		return invalid
	}
	var path []json.RawMessage
	if r, present := o["path"]; !present || json.Unmarshal(r, &path) != nil || len(path) == 0 {
		return invalid
	}
	for _, p := range path {
		if !nonEmptyRawString(p) {
			return invalid
		}
	}
	return nil
}

func validateDebugConfig(raw json.RawMessage, key string) error {
	invalid := fmt.Errorf("ACP lifecycle state data: %s.debug is invalid.", key) //nolint:staticcheck // mirrors the TS zod object
	o, ok := asRawObject(raw)
	if !ok {
		return invalid
	}
	if !optionalOK(o, "enabled", isRawBool) || !optionalOK(o, "level", enumRaw("error", "warn", "info", "debug", "trace")) {
		return invalid
	}
	return nil
}

func validateSubsystems(o rawObject) bool {
	return optionalOK(o, "subsystems", func(raw json.RawMessage) bool {
		var items []json.RawMessage
		if isJSONNull(raw) || json.Unmarshal(raw, &items) != nil {
			return false
		}
		for _, it := range items {
			if !isRawString(it) {
				return false
			}
		}
		return true
	})
}

func validateResponseFormat(raw json.RawMessage, key string) error {
	invalid := fmt.Errorf("ACP lifecycle state data: %s.responseFormat is invalid.", key) //nolint:staticcheck // mirrors the TS zod discriminated union
	o, ok := asRawObject(raw)
	if !ok {
		return invalid
	}
	var typ string
	if r, present := o["type"]; !present || json.Unmarshal(r, &typ) != nil {
		return invalid
	}
	switch typ {
	case "text":
		return nil
	case "json":
		if !optionalOK(o, "name", isRawString) || !optionalOK(o, "description", isRawString) {
			return invalid
		}
		// schema: record<string, json>, optional.
		if !optionalOK(o, "schema", func(r json.RawMessage) bool { _, ok := asRawObject(r); return ok }) {
			return invalid
		}
		return nil
	}
	return invalid
}

// validateStartConfigMembers runs the deep element checks of
// validateStartConfigShape. cold is true for a coldSession, whose schema omits
// prompt, debug, model and modelMapping.
func validateStartConfigMembers(cfg rawObject, key string, cold bool) error {
	if v, ok := cfg["tools"]; ok {
		var items []json.RawMessage
		_ = json.Unmarshal(v, &items)
		if err := validateToolSpecs(items, key); err != nil {
			return err
		}
	}
	if v, ok := cfg["builtinTools"]; ok {
		var items []json.RawMessage
		_ = json.Unmarshal(v, &items)
		if err := validateBuiltinTools(items, key); err != nil {
			return err
		}
	}
	if v, ok := cfg["permissionModeMapping"]; ok {
		if err := validatePermissionModeMapping(v, key); err != nil {
			return err
		}
	}
	if v, ok := cfg["outputSchemaMapping"]; ok {
		if err := validateOutputSchemaMapping(v, key); err != nil {
			return err
		}
	}
	if v, ok := cfg["responseFormat"]; ok {
		if err := validateResponseFormat(v, key); err != nil {
			return err
		}
	}
	if cold {
		return nil
	}
	if v, ok := cfg["model"]; ok && !isRawString(v) {
		return fmt.Errorf("ACP lifecycle state data: %s.model must be a string.", key) //nolint:staticcheck // mirrors the TS zod optional string
	}
	if v, ok := cfg["modelMapping"]; ok {
		if err := validateModelMapping(v, key); err != nil {
			return err
		}
	}
	if v, ok := cfg["debug"]; ok {
		if err := validateDebugConfig(v, key); err != nil {
			return err
		}
		o, _ := asRawObject(v)
		if !validateSubsystems(o) {
			return fmt.Errorf("ACP lifecycle state data: %s.debug is invalid.", key) //nolint:staticcheck // mirrors the TS zod object
		}
	}
	return nil
}

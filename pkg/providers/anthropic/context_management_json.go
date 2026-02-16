package anthropic

import (
	"encoding/json"
	"fmt"
)

// MarshalJSON implements custom JSON marshaling for ContextManagement
func (cm *ContextManagement) MarshalJSON() ([]byte, error) {
	type _Alias ContextManagement

	// Convert edits to JSON-serializable format
	var editsJSON []map[string]interface{}
	for _, edit := range cm.Edits {
		editMap, err := marshalEdit(edit)
		if err != nil {
			return nil, err
		}
		editsJSON = append(editsJSON, editMap)
	}

	return json.Marshal(map[string]interface{}{
		"edits": editsJSON,
	})
}

// MarshalJSON implements custom JSON marshaling for ClearThinkingEdit
func (e *ClearThinkingEdit) MarshalJSON() ([]byte, error) {
	result := map[string]interface{}{
		"type": e.Type,
	}

	if e.Keep != nil {
		switch k := e.Keep.(type) {
		case *KeepAllThinking:
			result["keep"] = "all"
		case *KeepRecentThinkingTurns:
			result["keep"] = map[string]interface{}{
				"type":  k.Type,
				"value": k.Value,
			}
		}
	}

	return json.Marshal(result)
}

// marshalEdit converts a ContextManagementEdit to a map for JSON serialization
func marshalEdit(edit ContextManagementEdit) (map[string]interface{}, error) {
	switch e := edit.(type) {
	case *ClearToolUsesEdit:
		return marshalClearToolUsesEdit(e)
	case *ClearThinkingEdit:
		return marshalClearThinkingEdit(e)
	case *CompactEdit:
		return marshalCompactEdit(e)
	default:
		return nil, fmt.Errorf("unknown edit type: %T", edit)
	}
}

// marshalClearToolUsesEdit converts ClearToolUsesEdit to map
func marshalClearToolUsesEdit(e *ClearToolUsesEdit) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"type": e.Type,
	}

	if e.Trigger != nil {
		result["trigger"] = map[string]interface{}{
			"type":  e.Trigger.Type,
			"value": e.Trigger.Value,
		}
	}

	if e.Keep != nil {
		result["keep"] = map[string]interface{}{
			"type":  e.Keep.Type,
			"value": e.Keep.Value,
		}
	}

	if e.ClearAtLeast != nil {
		result["clearAtLeast"] = map[string]interface{}{
			"type":  e.ClearAtLeast.Type,
			"value": e.ClearAtLeast.Value,
		}
	}

	if e.ClearToolInputs != nil {
		result["clearToolInputs"] = *e.ClearToolInputs
	}

	if len(e.ExcludeTools) > 0 {
		result["excludeTools"] = e.ExcludeTools
	}

	return result, nil
}

// marshalClearThinkingEdit converts ClearThinkingEdit to map
func marshalClearThinkingEdit(e *ClearThinkingEdit) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"type": e.Type,
	}

	// Handle the Keep field based on its concrete type
	if e.Keep != nil {
		switch k := e.Keep.(type) {
		case *KeepAllThinking:
			result["keep"] = "all"
		case *KeepRecentThinkingTurns:
			result["keep"] = map[string]interface{}{
				"type":  k.Type,
				"value": k.Value,
			}
		default:
			return nil, fmt.Errorf("unknown keep type: %T", k)
		}
	}

	return result, nil
}

// marshalCompactEdit converts CompactEdit to map
func marshalCompactEdit(e *CompactEdit) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"type": e.Type,
	}

	if e.Trigger != nil {
		result["trigger"] = map[string]interface{}{
			"type":  e.Trigger.Type,
			"value": e.Trigger.Value,
		}
	}

	if e.PauseAfterCompaction != nil {
		result["pauseAfterCompaction"] = *e.PauseAfterCompaction
	}

	if e.Instructions != nil {
		result["instructions"] = *e.Instructions
	}

	return result, nil
}

// UnmarshalJSON implements custom JSON unmarshaling for ContextManagementResponse
func (cmr *ContextManagementResponse) UnmarshalJSON(data []byte) error {
	var raw struct {
		AppliedEdits []json.RawMessage `json:"applied_edits"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Parse each applied edit based on its type
	cmr.AppliedEdits = make([]AppliedEdit, 0, len(raw.AppliedEdits))
	for _, rawEdit := range raw.AppliedEdits {
		// First, peek at the type
		var typeCheck struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rawEdit, &typeCheck); err != nil {
			return err
		}

		// Parse based on type
		var edit AppliedEdit
		switch typeCheck.Type {
		case "clear_tool_uses_20250919":
			var e AppliedClearToolUsesEdit
			if err := json.Unmarshal(rawEdit, &e); err != nil {
				return err
			}
			edit = &e

		case "clear_thinking_20251015":
			var e AppliedClearThinkingEdit
			if err := json.Unmarshal(rawEdit, &e); err != nil {
				return err
			}
			edit = &e

		case "compact_20260112":
			var e AppliedCompactEdit
			if err := json.Unmarshal(rawEdit, &e); err != nil {
				return err
			}
			edit = &e

		default:
			// Skip unknown edit types for forward compatibility
			continue
		}

		cmr.AppliedEdits = append(cmr.AppliedEdits, edit)
	}

	return nil
}

package acp

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// The ACP bridge validates the `start` frame (and the persisted
// `turnStartConfig` / `coldSession` lifecycle data) with zod schemas keyed by
// the TS field names, so every mapping type that crosses the wire is encoded
// in the TS shape (`ACPInstructionMapping`, `ACPOutputSchemaMapping`,
// `ACPModelMapping`, `ACPPermissionModeMapping`, `ACPAuthentication`).
// Decoding is case-insensitive, so lifecycle state persisted with the previous
// Go-field-name encoding still loads.

type instructionMappingWire struct {
	Type     InstructionMappingType `json:"type"`
	Path     json.RawMessage        `json:"path,omitempty"`
	Variable string                 `json:"variable,omitempty"`
}

// MarshalJSON encodes the TS discriminated union: `session-meta` and
// `launch-env-json` carry a `path` array, `filesystem` a `path` string.
func (m InstructionMapping) MarshalJSON() ([]byte, error) {
	w := instructionMappingWire{Type: m.Type}
	var err error
	switch m.Type {
	case InstructionMappingFilesystem:
		w.Path, err = json.Marshal(m.FilePath)
	case InstructionMappingLaunchEnvJSON:
		w.Variable = m.Variable
		w.Path, err = json.Marshal(nonNilStrings(m.Path))
	default:
		w.Path, err = json.Marshal(nonNilStrings(m.Path))
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(w)
}

// UnmarshalJSON decodes the TS shape (and the legacy Go-field-name shape).
func (m *InstructionMapping) UnmarshalJSON(data []byte) error {
	var w struct {
		Type     InstructionMappingType `json:"type"`
		Path     json.RawMessage        `json:"path"`
		Variable string                 `json:"variable"`
		FilePath string                 `json:"filePath"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*m = InstructionMapping{Type: w.Type, Variable: w.Variable, FilePath: w.FilePath}
	if len(w.Path) == 0 || bytes.Equal(w.Path, []byte("null")) {
		return nil
	}
	if w.Type == InstructionMappingFilesystem {
		var s string
		if err := json.Unmarshal(w.Path, &s); err != nil {
			return fmt.Errorf("ACP instruction mapping: filesystem path must be a string: %w", err)
		}
		m.FilePath = s
		return nil
	}
	return json.Unmarshal(w.Path, &m.Path)
}

// MarshalJSON encodes `{ type: 'session-prompt-meta', path }`.
func (m OutputSchemaMapping) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type string   `json:"type"`
		Path []string `json:"path"`
	}{Type: "session-prompt-meta", Path: nonNilStrings(m.Path)})
}

// UnmarshalJSON decodes the TS shape.
func (m *OutputSchemaMapping) UnmarshalJSON(data []byte) error {
	var w struct {
		Path []string `json:"path"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	m.Path = w.Path
	return nil
}

// MarshalJSON encodes the TS union: `session-mode` carries `modeId`,
// `session-config-option` carries `configId` and a string or boolean `value`.
func (t PermissionModeTarget) MarshalJSON() ([]byte, error) {
	if t.Type == PermissionTargetSessionConfigOption {
		return json.Marshal(struct {
			Type     PermissionModeTargetType `json:"type"`
			ConfigID string                   `json:"configId"`
			Value    any                      `json:"value"`
		}{t.Type, t.ConfigID, t.Value})
	}
	return json.Marshal(struct {
		Type   PermissionModeTargetType `json:"type"`
		ModeID string                   `json:"modeId"`
	}{t.Type, t.ModeID})
}

// UnmarshalJSON decodes the TS shape (and the legacy Go-field-name shape).
func (t *PermissionModeTarget) UnmarshalJSON(data []byte) error {
	var w struct {
		Type     PermissionModeTargetType `json:"type"`
		ModeID   string                   `json:"modeId"`
		ConfigID string                   `json:"configId"`
		Value    any                      `json:"value"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*t = PermissionModeTarget{Type: w.Type, ModeID: w.ModeID, ConfigID: w.ConfigID, Value: w.Value}
	return nil
}

// MarshalJSON encodes `{ 'allow-reads', 'allow-edits', 'allow-all' }`; a nil
// target is an explicit no-op and encodes as `null`.
func (m PermissionModeMapping) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		AllowReads *PermissionModeTarget `json:"allow-reads"`
		AllowEdits *PermissionModeTarget `json:"allow-edits"`
		AllowAll   *PermissionModeTarget `json:"allow-all"`
	}{m.AllowReads, m.AllowEdits, m.AllowAll})
}

// UnmarshalJSON decodes the TS shape (and the legacy Go-field-name shape).
func (m *PermissionModeMapping) UnmarshalJSON(data []byte) error {
	var w struct {
		AllowReads *PermissionModeTarget `json:"allow-reads"`
		AllowEdits *PermissionModeTarget `json:"allow-edits"`
		AllowAll   *PermissionModeTarget `json:"allow-all"`
		LegacyR    *PermissionModeTarget `json:"allowReads"`
		LegacyE    *PermissionModeTarget `json:"allowEdits"`
		LegacyA    *PermissionModeTarget `json:"allowAll"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	m.AllowReads, m.AllowEdits, m.AllowAll = firstTarget(w.AllowReads, w.LegacyR), firstTarget(w.AllowEdits, w.LegacyE), firstTarget(w.AllowAll, w.LegacyA)
	return nil
}

func firstTarget(a, b *PermissionModeTarget) *PermissionModeTarget {
	if a != nil {
		return a
	}
	return b
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

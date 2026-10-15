package acp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
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
// target is an explicit no-op and encodes as `null`. Modes flagged Unset* are left
// out, like a TS record with an undefined entry.
func (m PermissionModeMapping) MarshalJSON() ([]byte, error) {
	var w struct {
		AllowReads *PermissionModeTarget `json:"allow-reads"`
		AllowEdits *PermissionModeTarget `json:"allow-edits"`
		AllowAll   *PermissionModeTarget `json:"allow-all"`
	}
	if m.IsComplete() || (!m.UnsetAllowReads && !m.UnsetAllowEdits && !m.UnsetAllowAll) {
		w.AllowReads, w.AllowEdits, w.AllowAll = m.AllowReads, m.AllowEdits, m.AllowAll
		return json.Marshal(w)
	}
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	for _, e := range []struct {
		key  string
		mode harness.PermissionMode
		t    *PermissionModeTarget
	}{{"allow-reads", harness.PermissionModeAllowReads, m.AllowReads}, {"allow-edits", harness.PermissionModeAllowEdits, m.AllowEdits}, {"allow-all", harness.PermissionModeAllowAll, m.AllowAll}} {
		if m.isUnset(e.mode) {
			continue
		}
		v, err := json.Marshal(e.t)
		if err != nil {
			return nil, err
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		fmt.Fprintf(&b, "%q:%s", e.key, v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// UnmarshalJSON decodes the TS shape (and the legacy Go-field-name shape). A
// missing key is recorded in the Unset* flags; an explicit null is a no-op target.
func (m *PermissionModeMapping) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = PermissionModeMapping{}
	for _, e := range []struct {
		keys []string
		mode harness.PermissionMode
		dst  **PermissionModeTarget
		flag *bool
	}{
		{[]string{"allow-reads", "allowReads"}, harness.PermissionModeAllowReads, &m.AllowReads, &m.UnsetAllowReads},
		{[]string{"allow-edits", "allowEdits"}, harness.PermissionModeAllowEdits, &m.AllowEdits, &m.UnsetAllowEdits},
		{[]string{"allow-all", "allowAll"}, harness.PermissionModeAllowAll, &m.AllowAll, &m.UnsetAllowAll},
	} {
		found := false
		for _, k := range e.keys {
			msg, ok := lookupFold(raw, k)
			if !ok {
				continue
			}
			found = true
			if err := json.Unmarshal(msg, e.dst); err != nil {
				return err
			}
			if *e.dst != nil {
				break
			}
		}
		if !found {
			*e.flag = true
		}
	}
	return nil
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func lookupFold(raw map[string]json.RawMessage, key string) (json.RawMessage, bool) {
	if v, ok := raw[key]; ok {
		return v, true
	}
	for k, v := range raw {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

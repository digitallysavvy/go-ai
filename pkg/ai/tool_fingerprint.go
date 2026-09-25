package ai

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ToolDrift is the result of DetectToolDrift.
type ToolDrift struct {
	// Added lists tools present only in the current fingerprints.
	Added []string `json:"added"`
	// Removed lists tools present only in the baseline fingerprints.
	Removed []string `json:"removed"`
	// Changed lists tools whose pinned definition differs.
	Changed []string `json:"changed"`
}

// FingerprintTools fingerprints the server-controlled, security-relevant fields
// of each tool: description (string form only), the resolved input JSON
// schema, and title. It returns a map of tool name to a stable digest
// (SHA-256 of canonical JSON, base64url without padding).
//
// Capture a baseline at trust time (first connect, human-reviewed) and compare
// later fetches with DetectToolDrift to catch MCP tool-definition drift
// ("rug pull"). Digests are byte-identical to the TypeScript SDK's
// fingerprintTools (generate-text/tool-fingerprint.ts).
//
// A DescriptionFunc is developer-owned, so only its presence is pinned, not
// its output. An empty Title hashes like an undefined TS title.
func FingerprintTools(tools []types.Tool) (map[string]string, error) {
	out := make(map[string]string, len(tools))
	for _, tool := range tools {
		schema, err := fingerprintInputSchema(tool.Parameters)
		if err != nil {
			return nil, fmt.Errorf("fingerprint tool %q: %w", tool.Name, err)
		}
		var title interface{} = jsUndefined{}
		if tool.Title != "" {
			title = tool.Title
		}
		digest, err := hashFingerprintCanonical(map[string]interface{}{
			"description": tagToolDescription(tool),
			"inputSchema": schema,
			"title":       title,
		})
		if err != nil {
			return nil, fmt.Errorf("fingerprint tool %q: %w", tool.Name, err)
		}
		out[tool.Name] = digest
	}
	return out, nil
}

// DetectToolDrift diffs two fingerprint maps produced by FingerprintTools.
// Result slices are sorted by tool name.
func DetectToolDrift(current, baseline map[string]string) ToolDrift {
	drift := ToolDrift{Added: []string{}, Removed: []string{}, Changed: []string{}}
	for name, digest := range current {
		base, ok := baseline[name]
		if !ok {
			drift.Added = append(drift.Added, name)
		} else if digest != base {
			drift.Changed = append(drift.Changed, name)
		}
	}
	for name := range baseline {
		if _, ok := current[name]; !ok {
			drift.Removed = append(drift.Removed, name)
		}
	}
	sort.Strings(drift.Added)
	sort.Strings(drift.Removed)
	sort.Strings(drift.Changed)
	return drift
}

// tagToolDescription keeps a literal string equal to some placeholder from
// ever hashing like a function (TS tagDescription).
func tagToolDescription(tool types.Tool) map[string]interface{} {
	switch {
	case tool.DescriptionFunc != nil:
		return map[string]interface{}{"type": "function"}
	case tool.Description != "":
		return map[string]interface{}{"type": "string", "value": tool.Description}
	default:
		return map[string]interface{}{"type": "none"}
	}
}

// fingerprintInputSchema resolves a tool's input schema the way TS asSchema
// does: nil becomes the empty strict object schema.
func fingerprintInputSchema(parameters interface{}) (interface{}, error) {
	switch p := parameters.(type) {
	case nil:
		return map[string]interface{}{
			"type":                 "object",
			"properties":           map[string]interface{}{},
			"additionalProperties": false,
		}, nil
	case interface{ JSONSchema() map[string]interface{} }:
		return p.JSONSchema(), nil
	default:
		return parameters, nil
	}
}

// jsUndefined marks a JavaScript `undefined` object value. TS canonicalJSON
// interpolates it as the bare token `undefined`, which must be reproduced for
// digest parity.
type jsUndefined struct{}

func hashFingerprintCanonical(value interface{}) (string, error) {
	var buf bytes.Buffer
	if err := writeFingerprintCanonical(&buf, value); err != nil {
		return "", err
	}
	sum := sha256.Sum256(buf.Bytes())
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// writeFingerprintCanonical mirrors TS util/canonical-hash.ts canonicalJSON:
// object keys sorted by UTF-16 code unit order, JSON.stringify string and
// number formatting, undefined array elements as null.
func writeFingerprintCanonical(buf *bytes.Buffer, value interface{}) error {
	switch v := value.(type) {
	case jsUndefined:
		buf.WriteString("undefined")
	case nil:
		buf.WriteString("null")
	case string:
		writeJSString(buf, v)
	case bool:
		buf.WriteString(strconv.FormatBool(v))
	case json.Number:
		buf.WriteString(v.String())
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		buf.Write(b)
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return lessUTF16(keys[i], keys[j]) })
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeJSString(buf, k)
			buf.WriteByte(':')
			if err := writeFingerprintCanonical(buf, v[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case []interface{}:
		buf.WriteByte('[')
		for i, e := range v {
			if i > 0 {
				buf.WriteByte(',')
			}
			if _, undef := e.(jsUndefined); undef {
				buf.WriteString("null")
				continue
			}
			if err := writeFingerprintCanonical(buf, e); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	default:
		// Structs, typed maps and slices: normalize through encoding/json.
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var generic interface{}
		if err := dec.Decode(&generic); err != nil {
			return err
		}
		return writeFingerprintCanonical(buf, generic)
	}
	return nil
}

// writeJSString writes s exactly as JavaScript JSON.stringify would. Unlike
// encoding/json it does not escape <, >, &, U+2028 or U+2029.
func writeJSString(buf *bytes.Buffer, s string) {
	const hex = "0123456789abcdef"
	buf.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '"':
			buf.WriteString(`\"`)
		case r == '\\':
			buf.WriteString(`\\`)
		case r == '\b':
			buf.WriteString(`\b`)
		case r == '\f':
			buf.WriteString(`\f`)
		case r == '\n':
			buf.WriteString(`\n`)
		case r == '\r':
			buf.WriteString(`\r`)
		case r == '\t':
			buf.WriteString(`\t`)
		case r < 0x20:
			buf.WriteString(`\u00`)
			buf.WriteByte(hex[r>>4])
			buf.WriteByte(hex[r&0xf])
		default:
			buf.WriteString(s[i : i+size])
		}
		i += size
	}
	buf.WriteByte('"')
}

// lessUTF16 orders strings by UTF-16 code units, matching JS Array#sort.
func lessUTF16(a, b string) bool {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}

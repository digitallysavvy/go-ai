// Package jsstr is JSON.stringify for the harness: no HTML escaping and
// U+2028/U+2029 written as is (Go's encoder always escapes them).
package jsstr

import (
	"bytes"
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// Marshal is JSON.stringify(value).
func Marshal(v any) ([]byte, error) { return jsonstr.Marshal(v) }

// MarshalIndent is JSON.stringify(value, null, 2).
func MarshalIndent(v any) ([]byte, error) {
	b, err := jsonstr.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

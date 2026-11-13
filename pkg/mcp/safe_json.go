package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
)

const (
	// mcpMaxResponseBodyBytes bounds a JSON response read from an MCP server
	// (tool results can legitimately carry multi-megabyte base64 images).
	mcpMaxResponseBodyBytes = 64 << 20
	// mcpMaxErrorBodyBytes bounds the text kept from a failed HTTP response.
	mcpMaxErrorBodyBytes = 1 << 20
)

// readErrorBody reads at most mcpMaxErrorBodyBytes of an error response body.
func readErrorBody(r io.Reader) []byte {
	body, _ := io.ReadAll(io.LimitReader(r, mcpMaxErrorBodyBytes))
	return body
}

// readResponseBody reads a response body from an untrusted server, failing
// instead of buffering more than mcpMaxResponseBodyBytes.
func readResponseBody(r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, mcpMaxResponseBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > mcpMaxResponseBodyBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", mcpMaxResponseBodyBytes)
	}
	return body, nil
}

const (
	defaultJSONMaxDepth = 64
	// defaultJSONMaxFields is a backstop only: TS parses MCP messages with no
	// field limit, and a legitimate large result (a tools/list of a few
	// hundred tools with schemas, or a structuredContent list of records)
	// easily carries tens of thousands of object members. Memory is already
	// bounded by mcpMaxResponseBodyBytes.
	defaultJSONMaxFields = 1 << 22
)

// unmarshalSafeJSON decodes a server message the way TS secureJsonParse /
// parseJSONRPCMessage reads it: a forbidden prototype key fails, and the
// depth and field limits apply.
func unmarshalSafeJSON(data []byte, target interface{}) error {
	return unmarshalServerJSON(data, target, true)
}

// unmarshalPlainJSON is unmarshalSafeJSON for a body TS reads with plain
// JSON.parse (response.json(), or a result re-read after the transport):
// the depth and field limits still apply (Go memory guards), the prototype
// check does not. The final decode is still jsonexact.Unmarshal, which has
// its own prototype check until its non-checking entry point lands (round
// 97 CR to the integrator).
func unmarshalPlainJSON(data []byte, target interface{}) error {
	return unmarshalServerJSON(data, target, false)
}

func unmarshalServerJSON(data []byte, target interface{}, checkPrototype bool) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := checkServerJSON(data, checkPrototype); err != nil {
		return err
	}

	// Server messages decode with exact keys, as zod: a key whose only
	// match is case-insensitive is left out (a loose type keeps it as an
	// extra key).
	return jsonexact.Unmarshal(data, target)
}

// checkServerJSON parses data and applies the depth and field limits, and
// the prototype check when checkPrototype is set.
func checkServerJSON(data []byte, checkPrototype bool) error {
	var raw interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	fields := 0
	return rejectUnsafeJSONWalk(raw, 1, defaultJSONMaxDepth, defaultJSONMaxFields, &fields, checkPrototype)
}

// rejectUnsafeJSON applies the depth and field limits and the prototype
// check to a decoded value.
func rejectUnsafeJSON(value interface{}, maxDepth, maxFields int) error {
	fields := 0
	return rejectUnsafeJSONWalk(value, 1, maxDepth, maxFields, &fields, true)
}

func rejectUnsafeJSONWalk(value interface{}, depth, maxDepth, maxFields int, fields *int, checkPrototype bool) error {
	if depth > maxDepth {
		return fmt.Errorf("JSON nesting exceeds maximum depth %d", maxDepth)
	}

	switch v := value.(type) {
	case map[string]interface{}:
		if checkPrototype {
			if _, ok := v["__proto__"]; ok {
				return fmt.Errorf("Object contains forbidden prototype property") //nolint:staticcheck // matches TS SDK's exact error text
			}
			if constructor, ok := v["constructor"].(map[string]interface{}); ok {
				if _, hasPrototype := constructor["prototype"]; hasPrototype {
					return fmt.Errorf("Object contains forbidden prototype property") //nolint:staticcheck // matches TS SDK's exact error text
				}
			}
		}
		for _, nested := range v {
			*fields = *fields + 1
			if *fields > maxFields {
				return fmt.Errorf("JSON field count exceeds maximum %d", maxFields)
			}
			if err := rejectUnsafeJSONWalk(nested, depth+1, maxDepth, maxFields, fields, checkPrototype); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, nested := range v {
			if err := rejectUnsafeJSONWalk(nested, depth+1, maxDepth, maxFields, fields, checkPrototype); err != nil {
				return err
			}
		}
	}
	return nil
}

// drainBody discards the rest of a response body we do not use, so the
// connection can be reused. TS never reads such a body; reading is capped so
// a server that streams forever cannot hold the caller.
func drainBody(r io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(r, mcpMaxErrorBodyBytes))
}

package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	defaultJSONMaxDepth  = 64
	defaultJSONMaxFields = 4096
)

func unmarshalSafeJSON(data []byte, target interface{}) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}

	var raw interface{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&raw); err != nil {
		return err
	}
	if err := rejectUnsafeJSON(raw, defaultJSONMaxDepth, defaultJSONMaxFields); err != nil {
		return err
	}

	return json.Unmarshal(data, target)
}

func rejectUnsafeJSON(value interface{}, maxDepth, maxFields int) error {
	fields := 0
	return rejectUnsafeJSONWalk(value, 1, maxDepth, maxFields, &fields)
}

func rejectUnsafeJSONWalk(value interface{}, depth, maxDepth, maxFields int, fields *int) error {
	if depth > maxDepth {
		return fmt.Errorf("JSON nesting exceeds maximum depth %d", maxDepth)
	}

	switch v := value.(type) {
	case map[string]interface{}:
		if _, ok := v["__proto__"]; ok {
			return fmt.Errorf("Object contains forbidden prototype property") //nolint:staticcheck // matches TS SDK's exact error text
		}
		if constructor, ok := v["constructor"].(map[string]interface{}); ok {
			if _, hasPrototype := constructor["prototype"]; hasPrototype {
				return fmt.Errorf("Object contains forbidden prototype property") //nolint:staticcheck // matches TS SDK's exact error text
			}
		}
		for _, nested := range v {
			*fields = *fields + 1
			if *fields > maxFields {
				return fmt.Errorf("JSON field count exceeds maximum %d", maxFields)
			}
			if err := rejectUnsafeJSONWalk(nested, depth+1, maxDepth, maxFields, fields); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, nested := range v {
			if err := rejectUnsafeJSONWalk(nested, depth+1, maxDepth, maxFields, fields); err != nil {
				return err
			}
		}
	}
	return nil
}

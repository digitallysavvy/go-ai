package mcp

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// MCPHeaderValueType is the JSON Schema `type` an `x-mcp-header`-annotated
// property must declare.
type MCPHeaderValueType string

const (
	MCPHeaderValueBoolean MCPHeaderValueType = "boolean"
	MCPHeaderValueInteger MCPHeaderValueType = "integer"
	MCPHeaderValueString  MCPHeaderValueType = "string"
)

// MCPToolHeaderBinding binds one statically-reachable input schema property
// to an HTTP header sent with `tools/call` (hash 0c60a40).
type MCPToolHeaderBinding struct {
	HeaderName string
	Path       []string
	ValueType  MCPHeaderValueType
}

var (
	httpTokenPattern     = regexp.MustCompile(`^[!#$%&'*+\-.^_` + "`" + `|~0-9A-Za-z]+$`)
	base64SentinelHeader = regexp.MustCompile(`^=\?base64\?.*\?=$`)
)

// EncodeMCPHeaderValue encodes value for safe transport as an HTTP header:
// plain, already-trimmed, printable ASCII (tab or 0x20-0x7e) that doesn't
// itself look like the base64 sentinel is sent as-is; anything else
// (non-ASCII, leading/trailing whitespace, or a literal value matching the
// sentinel shape) is base64-encoded as `=?base64?<b64>?=`. Matches TS
// encodeMCPHeaderValue (mcp-http-headers.ts).
func EncodeMCPHeaderValue(value string) string {
	isPlainASCII := true
	for _, r := range value {
		if r != 0x09 && (r < 0x20 || r > 0x7e) {
			isPlainASCII = false
			break
		}
	}
	if isPlainASCII && strings.TrimSpace(value) == value && !base64SentinelHeader.MatchString(value) {
		return value
	}
	return "=?base64?" + base64.StdEncoding.EncodeToString([]byte(value)) + "?="
}

// GetMCPToolHeaderBindings walks a tool's JSON Schema input schema for
// `x-mcp-header`-annotated properties reachable only through `properties`
// nesting (an array's `items`, `additionalProperties`, or any other
// schema-composition keyword makes a property NOT statically reachable, and
// is rejected). Matches TS getMCPToolHeaderBindings (mcp-http-headers.ts,
// hash 0c60a40).
//
// Unlike TS, which returns bindings in property-declaration order, Go's
// input schema is an unordered map[string]interface{}, so the returned
// bindings are sorted by path for determinism (order carries no semantic
// meaning: createMCPToolHeaders builds an unordered header set from them).
func GetMCPToolHeaderBindings(inputSchema interface{}) (bindings []MCPToolHeaderBinding, err error) {
	root, ok := inputSchema.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("inputSchema must be a JSON Schema object")
	}

	headerNames := map[string]bool{}
	var visit func(value interface{}, path []string, staticallyReachable bool) error
	visit = func(value interface{}, path []string, staticallyReachable bool) error {
		node, ok := value.(map[string]interface{})
		if !ok {
			return nil
		}

		if rawHeaderName, present := node["x-mcp-header"]; present {
			if !staticallyReachable || len(path) == 0 {
				return fmt.Errorf("x-mcp-header is not on a statically reachable property")
			}
			headerName, ok := rawHeaderName.(string)
			if !ok || headerName == "" || !httpTokenPattern.MatchString(headerName) {
				return fmt.Errorf("x-mcp-header must be a non-empty HTTP token")
			}
			normalized := strings.ToLower(headerName)
			if headerNames[normalized] {
				return fmt.Errorf("x-mcp-header value %q is not unique", headerName)
			}

			valueTypeRaw, _ := node["type"].(string)
			valueType := MCPHeaderValueType(valueTypeRaw)
			if valueType != MCPHeaderValueBoolean && valueType != MCPHeaderValueInteger && valueType != MCPHeaderValueString {
				return fmt.Errorf("x-mcp-header can only annotate boolean, integer, or string properties")
			}

			headerNames[normalized] = true
			bindings = append(bindings, MCPToolHeaderBinding{HeaderName: headerName, Path: append([]string(nil), path...), ValueType: valueType})
		}

		for key, child := range node {
			if key == "x-mcp-header" {
				continue
			}
			if key == "properties" {
				if properties, ok := child.(map[string]interface{}); ok {
					for propertyName, propertySchema := range properties {
						if visitErr := visit(propertySchema, append(append([]string(nil), path...), propertyName), staticallyReachable); visitErr != nil {
							return visitErr
						}
					}
				}
				continue
			}
			if visitErr := visit(child, path, false); visitErr != nil {
				return visitErr
			}
		}
		return nil
	}

	if visitErr := visit(root, nil, true); visitErr != nil {
		return nil, visitErr
	}

	sort.Slice(bindings, func(i, j int) bool {
		return strings.Join(bindings[i].Path, ".") < strings.Join(bindings[j].Path, ".")
	})
	return bindings, nil
}

// getValueAtMCPHeaderPath walks args by path, returning nil if any segment
// is missing or not an object.
func getValueAtMCPHeaderPath(args map[string]interface{}, path []string) interface{} {
	var current interface{} = args
	for _, segment := range path {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = m[segment]
	}
	return current
}

// CreateMCPToolHeaders builds the `Mcp-Param-<HeaderName>` headers for a
// tools/call request from its bindings and arguments, matching TS
// createMCPToolHeaders (mcp-http-headers.ts, hash 0c60a40). A binding whose
// argument value is absent (nil) is skipped. It returns an error if an
// argument's runtime type does not match its binding's declared
// x-mcp-header type.
func CreateMCPToolHeaders(bindings []MCPToolHeaderBinding, args map[string]interface{}) (map[string]string, error) {
	headers := map[string]string{}
	for _, binding := range bindings {
		value := getValueAtMCPHeaderPath(args, binding.Path)
		if value == nil {
			continue
		}

		var stringValue string
		switch binding.ValueType {
		case MCPHeaderValueString:
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("tool argument %q does not match its x-mcp-header type", strings.Join(binding.Path, "."))
			}
			stringValue = s
		case MCPHeaderValueBoolean:
			b, ok := value.(bool)
			if !ok {
				return nil, fmt.Errorf("tool argument %q does not match its x-mcp-header type", strings.Join(binding.Path, "."))
			}
			stringValue = strconv.FormatBool(b)
		case MCPHeaderValueInteger:
			n, ok := mcpHeaderSafeInteger(value)
			if !ok {
				return nil, fmt.Errorf("tool argument %q does not match its x-mcp-header type", strings.Join(binding.Path, "."))
			}
			stringValue = strconv.FormatInt(n, 10)
		default:
			return nil, fmt.Errorf("unsupported x-mcp-header value type: %s", binding.ValueType)
		}

		headers["Mcp-Param-"+binding.HeaderName] = EncodeMCPHeaderValue(stringValue)
	}
	return headers, nil
}

// mcpHeaderSafeInteger accepts JSON-numeric values that represent a safe
// (no fractional part, within float64's exact integer range) integer,
// matching TS Number.isSafeInteger. json.Unmarshal into interface{} decodes
// numbers as float64; a hand-built args map may also use an int/int64
// directly.
func mcpHeaderSafeInteger(value interface{}) (int64, bool) {
	switch v := value.(type) {
	case float64:
		if v != float64(int64(v)) {
			return 0, false
		}
		const maxSafeInteger = 1 << 53
		if v > maxSafeInteger || v < -maxSafeInteger {
			return 0, false
		}
		return int64(v), true
	case int:
		return int64(v), true
	case int64:
		return v, true
	case int32:
		return int64(v), true
	default:
		return 0, false
	}
}

package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// validateMCPResultShape checks the required structure of a result the way the
// TS result schemas do (types.ts): a missing or mistyped required field fails
// the request with "Failed to parse server response" instead of decoding into
// zero values. Optional fields and unknown keys are left to the typed decode.
// Methods without a schema here pass unchanged.
func validateMCPResultShape(method string, raw json.RawMessage) error {
	var check func(obj map[string]json.RawMessage) error
	switch method {
	case "initialize":
		check = checkInitializeResult
	case "server/discover":
		check = checkDiscoverResult
	case "tools/list":
		check = func(obj map[string]json.RawMessage) error {
			return checkObjectArray(obj, "tools", checkToolDefinition)
		}
	case "resources/list":
		check = func(obj map[string]json.RawMessage) error {
			return checkObjectArray(obj, "resources", func(item map[string]json.RawMessage) error {
				return requireStrings(item, "uri", "name")
			})
		}
	case "resources/templates/list":
		check = func(obj map[string]json.RawMessage) error {
			return checkObjectArray(obj, "resourceTemplates", func(item map[string]json.RawMessage) error {
				return requireStrings(item, "uriTemplate", "name")
			})
		}
	case "resources/read":
		check = func(obj map[string]json.RawMessage) error {
			return checkObjectArray(obj, "contents", checkResourceContents)
		}
	case "prompts/list":
		check = func(obj map[string]json.RawMessage) error {
			return checkObjectArray(obj, "prompts", func(item map[string]json.RawMessage) error {
				return requireStrings(item, "name")
			})
		}
	case "prompts/get":
		check = func(obj map[string]json.RawMessage) error {
			return checkObjectArray(obj, "messages", checkPromptMessage)
		}
	case "completion/complete":
		check = checkCompleteResult
	default:
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return &MCPClientError{Message: "Failed to parse server response", Cause: fmt.Errorf("%s result must be an object", method)}
	}
	if err := check(obj); err != nil {
		return &MCPClientError{Message: "Failed to parse server response", Cause: fmt.Errorf("invalid %s result: %w", method, err)}
	}
	return nil
}

func isJSONString(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '"'
}

func isJSONObject(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '{'
}

func isJSONArray(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '['
}

func requireStrings(obj map[string]json.RawMessage, keys ...string) error {
	for _, key := range keys {
		if !isJSONString(obj[key]) {
			return fmt.Errorf("%s must be a string", key)
		}
	}
	return nil
}

func requireObject(obj map[string]json.RawMessage, key string) (map[string]json.RawMessage, error) {
	if !isJSONObject(obj[key]) {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(obj[key], &out); err != nil {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	return out, nil
}

func checkObjectArray(obj map[string]json.RawMessage, key string, checkItem func(map[string]json.RawMessage) error) error {
	if !isJSONArray(obj[key]) {
		return fmt.Errorf("%s must be an array", key)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(obj[key], &items); err != nil {
		return fmt.Errorf("%s must be an array", key)
	}
	for i, rawItem := range items {
		var item map[string]json.RawMessage
		if !isJSONObject(rawItem) || json.Unmarshal(rawItem, &item) != nil {
			return fmt.Errorf("%s[%d] must be an object", key, i)
		}
		if err := checkItem(item); err != nil {
			return fmt.Errorf("%s[%d]: %w", key, i, err)
		}
	}
	return nil
}

func checkImplementation(obj map[string]json.RawMessage, key string) error {
	impl, err := requireObject(obj, key)
	if err != nil {
		return err
	}
	return requireStrings(impl, "name", "version")
}

func checkInitializeResult(obj map[string]json.RawMessage) error {
	if err := requireStrings(obj, "protocolVersion"); err != nil {
		return err
	}
	if _, err := requireObject(obj, "capabilities"); err != nil {
		return err
	}
	return checkImplementation(obj, "serverInfo")
}

func checkDiscoverResult(obj map[string]json.RawMessage) error {
	if !isJSONArray(obj["supportedVersions"]) {
		return fmt.Errorf("supportedVersions must be an array")
	}
	var versions []json.RawMessage
	if err := json.Unmarshal(obj["supportedVersions"], &versions); err != nil {
		return fmt.Errorf("supportedVersions must be an array")
	}
	for _, v := range versions {
		if !isJSONString(v) {
			return fmt.Errorf("supportedVersions must contain strings")
		}
	}
	_, err := requireObject(obj, "capabilities")
	return err
}

func checkToolDefinition(tool map[string]json.RawMessage) error {
	if err := requireStrings(tool, "name"); err != nil {
		return err
	}
	inputSchema, err := requireObject(tool, "inputSchema")
	if err != nil {
		return err
	}
	if properties, ok := inputSchema["properties"]; ok && !isJSONObject(properties) {
		return fmt.Errorf("inputSchema.properties must be an object")
	}
	if outputSchema, ok := tool["outputSchema"]; ok && !isJSONObject(outputSchema) {
		return fmt.Errorf("outputSchema must be an object")
	}
	return nil
}

func checkResourceContents(contents map[string]json.RawMessage) error {
	if err := requireStrings(contents, "uri"); err != nil {
		return err
	}
	// TS union: text string, or blob that is valid base64 (z.base64()).
	blob, hasBlob := rawString(contents, "blob")
	if !isJSONString(contents["text"]) && (!hasBlob || !isBase64String(blob)) {
		return fmt.Errorf("contents must carry a text or blob string (blob must be base64)")
	}
	return nil
}

func checkPromptMessage(message map[string]json.RawMessage) error {
	var role string
	if err := json.Unmarshal(message["role"], &role); err != nil || (role != "user" && role != "assistant") {
		return fmt.Errorf(`role must be "user" or "assistant"`)
	}
	content, err := requireObject(message, "content")
	if err != nil {
		return err
	}
	if err := requireStrings(content, "type"); err != nil {
		return err
	}
	// TS PromptMessageSchema.content is a union of the four known content
	// types only (no unknown-type fallback, unlike tool results).
	typ, _ := rawString(content, "type")
	switch typ {
	case "text", "image", "resource", "resource_link":
		return validateToolContentBlock(content)
	}
	return fmt.Errorf("content type %q is not text, image, resource or resource_link", typ)
}

func checkCompleteResult(obj map[string]json.RawMessage) error {
	completion, err := requireObject(obj, "completion")
	if err != nil {
		return err
	}
	if !isJSONArray(completion["values"]) {
		return fmt.Errorf("completion.values must be an array")
	}
	var values []json.RawMessage
	if err := json.Unmarshal(completion["values"], &values); err != nil {
		return fmt.Errorf("completion.values must be an array")
	}
	if len(values) > 100 {
		return fmt.Errorf("completion.values must contain at most 100 items")
	}
	for _, v := range values {
		if !isJSONString(v) {
			return fmt.Errorf("completion.values must contain strings")
		}
	}
	return nil
}

package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// This file ports anthropicChunkSchema (anthropic-api.ts), the zod schema TS
// applies to every parsed streaming frame. zod objects are non-strict (unknown
// keys pass), `nullish` means absent or null, `optional` means absent only,
// and `nullable` means the key must be present but may be null. A frame that
// does not match becomes an `error` part (TypeValidationError in TS) and the
// stream keeps reading.

type zodIssue struct {
	path    []interface{}
	code    string
	message string
}

// zodNode validates one value; present is false when the object key is absent.
type zodNode func(v interface{}, present bool, path []interface{}) *zodIssue

func jsType(v interface{}, present bool) string {
	switch x := v.(type) {
	case nil:
		if !present {
			return "undefined"
		}
		return "null"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		_ = x
		return "unknown"
	}
}

func invalidType(expected string, v interface{}, present bool, path []interface{}) *zodIssue {
	return &zodIssue{path: path, code: "invalid_type", message: "Invalid input: expected " + expected + ", received " + jsType(v, present)}
}

func zString(v interface{}, present bool, path []interface{}) *zodIssue {
	if _, ok := v.(string); !ok {
		return invalidType("string", v, present, path)
	}
	return nil
}

func zNumber(v interface{}, present bool, path []interface{}) *zodIssue {
	if _, ok := v.(float64); !ok {
		return invalidType("number", v, present, path)
	}
	return nil
}

func zBoolean(v interface{}, present bool, path []interface{}) *zodIssue {
	if _, ok := v.(bool); !ok {
		return invalidType("boolean", v, present, path)
	}
	return nil
}

// zUnknown is z.unknown() / z.json(): any value passes.
func zUnknown(interface{}, bool, []interface{}) *zodIssue { return nil }

func zLiteral(want string) zodNode {
	return func(v interface{}, present bool, path []interface{}) *zodIssue {
		if s, ok := v.(string); ok && s == want {
			return nil
		}
		return &zodIssue{path: path, code: "invalid_value", message: fmt.Sprintf("Invalid input: expected %q", want)}
	}
}

func zNullish(n zodNode) zodNode {
	return func(v interface{}, present bool, path []interface{}) *zodIssue {
		if !present || v == nil {
			return nil
		}
		return n(v, present, path)
	}
}

func zNullable(n zodNode) zodNode {
	return func(v interface{}, present bool, path []interface{}) *zodIssue {
		if present && v == nil {
			return nil
		}
		return n(v, present, path)
	}
}

func zOptional(n zodNode) zodNode {
	return func(v interface{}, present bool, path []interface{}) *zodIssue {
		if !present {
			return nil
		}
		return n(v, present, path)
	}
}

func zArray(n zodNode) zodNode {
	return func(v interface{}, present bool, path []interface{}) *zodIssue {
		a, ok := v.([]interface{})
		if !ok {
			return invalidType("array", v, present, path)
		}
		for i, e := range a {
			if bad := n(e, true, append(append([]interface{}{}, path...), i)); bad != nil {
				return bad
			}
		}
		return nil
	}
}

type zField struct {
	key  string
	node zodNode
}

func zObject(fields ...zField) zodNode {
	return func(v interface{}, present bool, path []interface{}) *zodIssue {
		m, ok := v.(map[string]interface{})
		if !ok {
			return invalidType("object", v, present, path)
		}
		for _, f := range fields {
			fv, fp := m[f.key]
			if bad := f.node(fv, fp, append(append([]interface{}{}, path...), f.key)); bad != nil {
				return bad
			}
		}
		return nil
	}
}

// zRecord is z.record(z.string(), z.unknown()): an object with any values.
func zRecord(v interface{}, present bool, path []interface{}) *zodIssue {
	if _, ok := v.(map[string]interface{}); !ok {
		return invalidType("record", v, present, path)
	}
	return nil
}

func zUnion(nodes ...zodNode) zodNode {
	return func(v interface{}, present bool, path []interface{}) *zodIssue {
		for _, n := range nodes {
			if n(v, present, path) == nil {
				return nil
			}
		}
		return &zodIssue{path: path, code: "invalid_union", message: "Invalid input"}
	}
}

// zDisc is z.discriminatedUnion('type', ...).
func zDisc(variants map[string]zodNode) zodNode {
	return func(v interface{}, present bool, path []interface{}) *zodIssue {
		m, ok := v.(map[string]interface{})
		if !ok {
			return invalidType("object", v, present, path)
		}
		t, _ := m["type"].(string)
		n, known := variants[t]
		if !known {
			return &zodIssue{path: append(append([]interface{}{}, path...), "type"), code: "invalid_union", message: "Invalid input"}
		}
		return n(v, present, path)
	}
}

func typed(t string, fields ...zField) zodNode {
	return zObject(append([]zField{{"type", zLiteral(t)}}, fields...)...)
}

func f(key string, n zodNode) zField { return zField{key, n} }

var (
	zCaller = zUnion(
		typed("code_execution_20250825", f("tool_id", zString)),
		typed("code_execution_20260120", f("tool_id", zString)),
		typed("direct"),
	)

	zMcpToolResultContent = zUnion(
		zString,
		zArray(zUnion(
			zString,
			typed("text", f("text", zString), f("citations", zOptional(zNullable(zArray(zUnknown))))),
		)),
	)

	zInputTransformation = zObject(f("type", zString), f("path", zString), f("reason", zString))

	zStopDetails = zObject(
		f("type", zString),
		f("category", zNullish(zString)),
		f("explanation", zNullish(zString)),
		f("recommended_model", zNullish(zString)),
	)

	zSafeguardResult = zObject(
		f("type", zString),
		f("status", zObject(
			f("type", zString),
			f("tool_uses", zNullish(func(v interface{}, present bool, path []interface{}) *zodIssue {
				m, ok := v.(map[string]interface{})
				if !ok {
					return invalidType("record", v, present, path)
				}
				entry := zObject(f("type", zString), f("outcome", zNullish(zString)), f("explanation", zNullish(zString)))
				for k, e := range m {
					if bad := entry(e, true, append(append([]interface{}{}, path...), k)); bad != nil {
						return bad
					}
				}
				return nil
			})),
		)),
	)

	zCitation = zDisc(map[string]zodNode{
		"web_search_result_location": typed("web_search_result_location",
			f("cited_text", zString), f("url", zString), f("title", zNullable(zString)), f("encrypted_index", zString)),
		"page_location": typed("page_location",
			f("cited_text", zString), f("document_index", zNumber), f("document_title", zNullable(zString)),
			f("start_page_number", zNumber), f("end_page_number", zNumber), f("file_id", zNullish(zString))),
		"char_location": typed("char_location",
			f("cited_text", zString), f("document_index", zNumber), f("document_title", zNullable(zString)),
			f("start_char_index", zNumber), f("end_char_index", zNumber), f("file_id", zNullish(zString))),
		"content_block_location": typed("content_block_location",
			f("cited_text", zString), f("document_index", zNumber), f("document_title", zNullable(zString)),
			f("start_block_index", zNumber), f("end_block_index", zNumber), f("file_id", zNullable(zString))),
		"search_result_location": typed("search_result_location",
			f("cited_text", zString), f("search_result_index", zNumber), f("source", zString), f("title", zNullable(zString)),
			f("start_block_index", zNumber), f("end_block_index", zNumber)),
	})

	zCodeExecOutputs = zOptional(zArray(typed("code_execution_output", f("file_id", zString))))

	zContentBlockStart = zDisc(map[string]zodNode{
		"text":     typed("text", f("text", zString)),
		"thinking": typed("thinking", f("thinking", zString)),
		"tool_use": typed("tool_use",
			f("id", zString), f("name", zString), f("input", zOptional(zRecord)),
			f("caller", zOptional(zCaller)), f("toolset_name", zNullish(zString))),
		"redacted_thinking": typed("redacted_thinking", f("data", zString)),
		"compaction":        typed("compaction", f("content", zNullish(zString)), f("signature", zNullish(zString))),
		"server_tool_use": typed("server_tool_use",
			f("id", zString), f("name", zString), f("input", zNullish(zRecord)), f("caller", zOptional(zCaller))),
		"mcp_tool_use": typed("mcp_tool_use",
			f("id", zString), f("name", zString), f("input", zUnknown), f("server_name", zString)),
		"mcp_tool_result": typed("mcp_tool_result",
			f("tool_use_id", zString), f("is_error", zBoolean), f("content", zMcpToolResultContent)),
		"web_fetch_tool_result": typed("web_fetch_tool_result",
			f("tool_use_id", zString), f("caller", zOptional(zCaller)),
			f("content", zUnion(
				typed("web_fetch_result",
					f("url", zString), f("retrieved_at", zNullable(zString)),
					f("content", typed("document",
						f("title", zNullable(zString)),
						f("citations", zOptional(zObject(f("enabled", zBoolean)))),
						f("source", zUnion(
							typed("base64", f("media_type", zLiteral("application/pdf")), f("data", zString)),
							typed("text", f("media_type", zLiteral("text/plain")), f("data", zString)),
						)),
					))),
				typed("web_fetch_tool_result_error", f("error_code", zString)),
			))),
		"web_search_tool_result": typed("web_search_tool_result",
			f("tool_use_id", zString), f("caller", zOptional(zCaller)),
			f("content", zUnion(
				zArray(typed("web_search_result",
					f("url", zString), f("title", zNullable(zString)),
					f("encrypted_content", zString), f("page_age", zNullish(zString)))),
				typed("web_search_tool_result_error", f("error_code", zString)),
			))),
		"code_execution_tool_result": typed("code_execution_tool_result",
			f("tool_use_id", zString),
			f("content", zUnion(
				typed("code_execution_result",
					f("stdout", zString), f("stderr", zString), f("return_code", zNumber), f("content", zCodeExecOutputs)),
				typed("encrypted_code_execution_result",
					f("encrypted_stdout", zString), f("stderr", zString), f("return_code", zNumber), f("content", zCodeExecOutputs)),
				typed("code_execution_tool_result_error", f("error_code", zString)),
			))),
		"bash_code_execution_tool_result": typed("bash_code_execution_tool_result",
			f("tool_use_id", zString),
			f("content", zDisc(map[string]zodNode{
				"bash_code_execution_result": typed("bash_code_execution_result",
					f("content", zArray(typed("bash_code_execution_output", f("file_id", zString)))),
					f("stdout", zString), f("stderr", zString), f("return_code", zNumber)),
				"bash_code_execution_tool_result_error": typed("bash_code_execution_tool_result_error", f("error_code", zString)),
			}))),
		"text_editor_code_execution_tool_result": typed("text_editor_code_execution_tool_result",
			f("tool_use_id", zString),
			f("content", zDisc(map[string]zodNode{
				"text_editor_code_execution_tool_result_error": typed("text_editor_code_execution_tool_result_error", f("error_code", zString)),
				"text_editor_code_execution_view_result": typed("text_editor_code_execution_view_result",
					f("content", zString), f("file_type", zString),
					f("num_lines", zNullable(zNumber)), f("start_line", zNullable(zNumber)), f("total_lines", zNullable(zNumber))),
				"text_editor_code_execution_create_result": typed("text_editor_code_execution_create_result", f("is_file_update", zBoolean)),
				"text_editor_code_execution_str_replace_result": typed("text_editor_code_execution_str_replace_result",
					f("lines", zNullable(zArray(zString))), f("new_lines", zNullable(zNumber)), f("new_start", zNullable(zNumber)),
					f("old_lines", zNullable(zNumber)), f("old_start", zNullable(zNumber))),
			}))),
		"tool_search_tool_result": typed("tool_search_tool_result",
			f("tool_use_id", zString),
			f("content", zUnion(
				typed("tool_search_tool_search_result",
					f("tool_references", zArray(typed("tool_reference", f("tool_name", zString))))),
				typed("tool_search_tool_result_error", f("error_code", zString)),
			))),
		"advisor_tool_result": typed("advisor_tool_result",
			f("tool_use_id", zString),
			f("content", zDisc(map[string]zodNode{
				"advisor_result":            typed("advisor_result", f("text", zString), f("stop_reason", zNullish(zString))),
				"advisor_redacted_result":   typed("advisor_redacted_result", f("encrypted_content", zString), f("stop_reason", zNullish(zString))),
				"advisor_tool_result_error": typed("advisor_tool_result_error", f("error_code", zString)),
			}))),
		"fallback": typed("fallback",
			f("from", zObject(f("model", zString))), f("to", zObject(f("model", zString)))),
	})

	zContainerIDExpiry = zObject(f("expires_at", zString), f("id", zString))

	zIterationType = zUnion(zLiteral("compaction"), zLiteral("message"), zLiteral("advisor_message"), zLiteral("fallback_message"))

	zAppliedEdit = zUnion(
		typed("clear_tool_uses_20250919", f("cleared_tool_uses", zNumber), f("cleared_input_tokens", zNumber)),
		typed("clear_thinking_20251015", f("cleared_thinking_turns", zNumber), f("cleared_input_tokens", zNumber)),
		typed("compact_20260112"),
	)

	zAnthropicChunk = zDisc(map[string]zodNode{
		"message_start": typed("message_start", f("message", zObject(
			f("id", zNullish(zString)), f("model", zNullish(zString)), f("role", zNullish(zString)),
			f("usage", zObject(
				f("input_tokens", zNumber),
				f("cache_creation_input_tokens", zNullish(zNumber)),
				f("cache_read_input_tokens", zNullish(zNumber)))),
			f("content", zNullish(zArray(typed("tool_use",
				f("id", zString), f("name", zString), f("input", zUnknown),
				f("caller", zOptional(zCaller)), f("toolset_name", zNullish(zString)))))),
			f("stop_reason", zNullish(zString)),
			f("input_transformations", zNullish(zArray(zInputTransformation))),
			f("container", zNullish(zContainerIDExpiry)),
		))),
		"content_block_start": typed("content_block_start", f("index", zNumber), f("content_block", zContentBlockStart)),
		"content_block_delta": typed("content_block_delta", f("index", zNumber), f("delta", zDisc(map[string]zodNode{
			"input_json_delta": typed("input_json_delta", f("partial_json", zString)),
			"text_delta":       typed("text_delta", f("text", zString)),
			"thinking_delta":   typed("thinking_delta", f("thinking", zString)),
			"signature_delta":  typed("signature_delta", f("signature", zString)),
			"compaction_delta": typed("compaction_delta", f("content", zNullish(zString))),
			"citations_delta":  typed("citations_delta", f("citation", zCitation)),
		}))),
		"content_block_stop": typed("content_block_stop", f("index", zNumber)),
		"error": typed("error", f("error", zObject(
			f("type", zString), f("message", zString),
			f("code", zNullish(zUnion(zString, zNumber))),
			f("statusCode", zNullish(zNumber)),
			f("isRetryable", zNullish(zBoolean)),
			f("data", zUnknown),
		))),
		"message_delta": typed("message_delta",
			f("delta", zObject(
				f("stop_reason", zNullish(zString)),
				f("stop_sequence", zNullish(zString)),
				f("stop_details", zNullish(zStopDetails)),
				f("safeguard_results", zNullish(zArray(zSafeguardResult))),
				f("container", zNullish(zObject(
					f("expires_at", zString), f("id", zString),
					f("skills", zNullish(zArray(zObject(
						f("type", zUnion(zLiteral("anthropic"), zLiteral("custom"))),
						f("skill_id", zString), f("version", zString))))),
				))),
			)),
			f("usage", zObject(
				f("input_tokens", zNullish(zNumber)),
				f("output_tokens", zNumber),
				f("output_tokens_details", zNullish(zObject(f("thinking_tokens", zNullish(zNumber))))),
				f("cache_creation_input_tokens", zNullish(zNumber)),
				f("cache_read_input_tokens", zNullish(zNumber)),
				f("iterations", zNullish(zArray(zObject(
					f("type", zIterationType), f("model", zNullish(zString)),
					f("input_tokens", zNumber), f("output_tokens", zNumber),
					f("cache_creation_input_tokens", zNullish(zNumber)), f("cache_read_input_tokens", zNullish(zNumber)))))),
			)),
			f("input_transformations", zNullish(zArray(zInputTransformation))),
			f("context_management", zNullish(zObject(f("applied_edits", zArray(zAppliedEdit))))),
		),
		"message_stop": typed("message_stop"),
		"ping":         typed("ping"),
	})
)

// anthropicChunkValidationMessage validates a JSON frame against
// anthropicChunkSchema. It returns "" when the frame matches, otherwise the
// TS TypeValidationError message. The zod issue text is a best-effort
// rendering (path plus the zod v4 message for the first failing field).
func anthropicChunkValidationMessage(data string) string {
	var value interface{}
	if err := json.Unmarshal([]byte(data), &value); err != nil {
		return ""
	}
	bad := zAnthropicChunk(value, true, nil)
	if bad == nil {
		return ""
	}
	path := bad.path
	if path == nil {
		path = []interface{}{}
	}
	var issues bytes.Buffer
	enc := json.NewEncoder(&issues)
	enc.SetEscapeHTML(false)
	_ = enc.Encode([]map[string]interface{}{{"code": bad.code, "path": path, "message": bad.message}})
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(data)); err != nil {
		compact.WriteString(data)
	}
	return "Type validation failed: Value: " + compact.String() + ".\nError message: " + strings.TrimSpace(issues.String())
}

// anthropicChunkType returns the frame's `type` for dispatch.
func anthropicChunkType(data string) string {
	var head struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal([]byte(data), &head)
	return head.Type
}

func compactJSON(data string) string {
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(data)); err != nil {
		return data
	}
	return b.String()
}

// validationIssueText returns the issue list that follows "Error message: "
// in a validation message, or the whole message when the marker is absent.
func validationIssueText(message string) string {
	if _, after, ok := strings.Cut(message, "Error message: "); ok {
		return after
	}
	return message
}

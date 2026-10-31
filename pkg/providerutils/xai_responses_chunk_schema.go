package providerutils

import (
	"encoding/json"
	"net/http"
)

// Port of xaiResponsesChunkSchema (xai/src/responses/xai-responses-api.ts): a
// closed union, so an unlisted event type, or a listed one whose members do
// not match, fails the parse. Combinators live in chunk_schema.go.

var (
	zXaiAnnotation = zUnionOf(
		zObj(fl("type", zLit("url_citation")), fl("url", zStr), fl("title", zOptional(zStr))),
		zObj(fl("type", zStr)),
	)
	zXaiPart = zObj(fl("type", zStr), fl("text", zOptional(zStr)), fl("logprobs", zOptional(zArr(zAny))),
		fl("annotations", zOptional(zArr(zXaiAnnotation))))
	zXaiSummaryPart = zObj(fl("type", zStr), fl("text", zStr))

	zXaiToolCallFields = func(typ string) zNode {
		return zObj(fl("type", zLit(typ)), fl("name", zOptional(zStr)), fl("arguments", zOptional(zStr)), fl("input", zOptional(zStr)),
			fl("call_id", zOptional(zStr)), fl("id", zStr), fl("status", zStr), fl("action", zAny))
	}

	zXaiOutputItem = zDiscType(map[string]zNode{
		"web_search_call":       zXaiToolCallFields("web_search_call"),
		"x_search_call":         zXaiToolCallFields("x_search_call"),
		"code_interpreter_call": zXaiToolCallFields("code_interpreter_call"),
		"code_execution_call":   zXaiToolCallFields("code_execution_call"),
		"view_image_call":       zXaiToolCallFields("view_image_call"),
		"view_x_video_call":     zXaiToolCallFields("view_x_video_call"),
		"file_search_call": zObj(fl("type", zLit("file_search_call")), fl("id", zStr), fl("status", zStr), fl("queries", zOptional(zArr(zStr))),
			fl("results", zNullish(zArr(zObj(fl("file_id", zStr), fl("filename", zStr), fl("score", zNum), fl("text", zStr)))))),
		"image_generation_call": zObj(fl("type", zLit("image_generation_call")), fl("id", zStr), fl("status", zStr), fl("prompt", nStr), fl("result", nStr)),
		"custom_tool_call":      zXaiToolCallFields("custom_tool_call"),
		"mcp_call": zObj(fl("type", zLit("mcp_call")), fl("name", zOptional(zStr)), fl("arguments", zOptional(zStr)), fl("output", zOptional(zStr)),
			fl("error", zOptional(zStr)), fl("id", zStr), fl("status", zStr), fl("server_label", zOptional(zStr))),
		"message":       zObj(fl("type", zLit("message")), fl("role", zStr), fl("content", zArr(zXaiPart)), fl("id", zStr), fl("status", zStr)),
		"function_call": zObj(fl("type", zLit("function_call")), fl("name", zStr), fl("arguments", zStr), fl("call_id", zStr), fl("id", zStr)),
		"reasoning": zObj(fl("type", zLit("reasoning")), fl("id", zStr), fl("summary", zArr(zXaiSummaryPart)),
			fl("content", zNullish(zArr(zObj(fl("type", zStr), fl("text", zStr))))), fl("status", zStr), fl("encrypted_content", nStr)),
	})

	zXaiUsage = zObj(
		fl("input_tokens", zNum), fl("output_tokens", zNum), fl("total_tokens", zOptional(zNum)),
		fl("input_tokens_details", zOptional(zObj(fl("cached_tokens", zOptional(zNum))))),
		fl("output_tokens_details", zOptional(zObj(fl("reasoning_tokens", zOptional(zNum))))),
		fl("num_sources_used", zOptional(zNum)), fl("num_server_side_tools_used", zOptional(zNum)), fl("cost_in_usd_ticks", nNum),
	)

	zXaiResponseFields = func(partial bool) zNode {
		usage, status := zNullish(zXaiUsage), zNode(zStr)
		if partial {
			status = zOptional(zStr)
		}
		return zObj(
			fl("id", nStr), fl("created_at", nNum), fl("model", nStr), fl("object", zLit("response")),
			fl("output", zArr(zXaiOutputItem)), fl("usage", usage), fl("status", status),
			fl("service_tier", nStr), fl("prompt_cache_key", nStr), fl("safety_identifier", nStr))
	}

	zXaiItemOut = func(typ string, extra ...zFld) zNode {
		return zObj(append([]zFld{fl("type", zLit(typ)), fl("item_id", zStr), fl("output_index", zNum)}, extra...)...)
	}

	zXaiChunk = func() map[string]zNode {
		m := map[string]zNode{
			"response.created":            zObj(fl("type", zLit("response.created")), fl("response", zXaiResponseFields(true))),
			"response.in_progress":        zObj(fl("type", zLit("response.in_progress")), fl("response", zXaiResponseFields(true))),
			"response.output_item.added":  zObj(fl("type", zLit("response.output_item.added")), fl("item", zXaiOutputItem), fl("output_index", zNum)),
			"response.output_item.done":   zObj(fl("type", zLit("response.output_item.done")), fl("item", zXaiOutputItem), fl("output_index", zNum)),
			"response.content_part.added": zXaiItemOut("response.content_part.added", fl("content_index", zNum), fl("part", zXaiPart)),
			"response.content_part.done":  zXaiItemOut("response.content_part.done", fl("content_index", zNum), fl("part", zXaiPart)),
			"response.output_text.delta": zXaiItemOut("response.output_text.delta", fl("content_index", zNum), fl("delta", zStr),
				fl("logprobs", zOptional(zArr(zAny)))),
			"response.output_text.done": zXaiItemOut("response.output_text.done", fl("content_index", zNum), fl("text", zStr),
				fl("logprobs", zOptional(zArr(zAny))), fl("annotations", zOptional(zArr(zXaiAnnotation)))),
			"response.output_text.annotation.added": zXaiItemOut("response.output_text.annotation.added", fl("content_index", zNum),
				fl("annotation_index", zNum), fl("annotation", zXaiAnnotation)),
			"response.reasoning_summary_part.added":     zXaiItemOut("response.reasoning_summary_part.added", fl("summary_index", zNum), fl("part", zXaiSummaryPart)),
			"response.reasoning_summary_part.done":      zXaiItemOut("response.reasoning_summary_part.done", fl("summary_index", zNum), fl("part", zXaiSummaryPart)),
			"response.reasoning_summary_text.delta":     zXaiItemOut("response.reasoning_summary_text.delta", fl("summary_index", zNum), fl("delta", zStr)),
			"response.reasoning_summary_text.done":      zXaiItemOut("response.reasoning_summary_text.done", fl("summary_index", zNum), fl("text", zStr)),
			"response.reasoning_text.delta":             zXaiItemOut("response.reasoning_text.delta", fl("content_index", zNum), fl("delta", zStr)),
			"response.reasoning_text.done":              zXaiItemOut("response.reasoning_text.done", fl("content_index", zNum), fl("text", zStr)),
			"response.code_interpreter_call_code.delta": zXaiItemOut("response.code_interpreter_call_code.delta", fl("delta", zStr)),
			"response.code_interpreter_call_code.done":  zXaiItemOut("response.code_interpreter_call_code.done", fl("code", zStr)),
			"response.custom_tool_call_input.delta":     zXaiItemOut("response.custom_tool_call_input.delta", fl("delta", zStr)),
			"response.custom_tool_call_input.done":      zXaiItemOut("response.custom_tool_call_input.done", fl("input", zStr)),
			"response.function_call_arguments.delta":    zXaiItemOut("response.function_call_arguments.delta", fl("delta", zStr)),
			"response.function_call_arguments.done":     zXaiItemOut("response.function_call_arguments.done", fl("arguments", zStr)),
			"response.mcp_call_arguments.delta":         zXaiItemOut("response.mcp_call_arguments.delta", fl("delta", zStr)),
			"response.mcp_call_arguments.done":          zXaiItemOut("response.mcp_call_arguments.done", fl("arguments", zOptional(zStr))),
			"response.mcp_call_output.delta":            zXaiItemOut("response.mcp_call_output.delta", fl("delta", zStr)),
			"response.mcp_call_output.done":             zXaiItemOut("response.mcp_call_output.done", fl("output", zOptional(zStr))),
			"response.incomplete": zObj(fl("type", zLit("response.incomplete")), fl("response", zObj(
				fl("incomplete_details", zNullish(zObj(fl("reason", zStr)))), fl("usage", zNullish(zXaiUsage)),
				fl("service_tier", nStr), fl("prompt_cache_key", nStr), fl("safety_identifier", nStr)))),
			"response.failed": zObj(fl("type", zLit("response.failed")), fl("response", zObj(
				fl("error", zNullish(zObj(fl("code", nStr), fl("message", zStr)))),
				fl("incomplete_details", zNullish(zObj(fl("reason", zStr)))), fl("usage", zNullish(zXaiUsage))))),
			"error":              zObj(fl("type", zLit("error")), fl("code", nStr), fl("message", zStr), fl("param", nStr)),
			"response.done":      zObj(fl("type", zLit("response.done")), fl("response", zXaiResponseFields(false))),
			"response.completed": zObj(fl("type", zLit("response.completed")), fl("response", zXaiResponseFields(false))),
		}
		// Progress events carrying only item_id and output_index.
		for _, t := range []string{
			"response.web_search_call.in_progress", "response.web_search_call.searching", "response.web_search_call.completed",
			"response.x_search_call.in_progress", "response.x_search_call.searching", "response.x_search_call.completed",
			"response.file_search_call.in_progress", "response.file_search_call.searching", "response.file_search_call.completed",
			"response.image_generation_call.in_progress", "response.image_generation_call.generating", "response.image_generation_call.completed",
			"response.code_execution_call.in_progress", "response.code_execution_call.executing", "response.code_execution_call.completed",
			"response.code_interpreter_call.in_progress", "response.code_interpreter_call.executing",
			"response.code_interpreter_call.interpreting", "response.code_interpreter_call.completed",
			"response.mcp_call.in_progress", "response.mcp_call.executing", "response.mcp_call.completed", "response.mcp_call.failed",
		} {
			m[t] = zXaiItemOut(t)
		}
		return m
	}()
)

// ValidateXAIResponsesChunk checks an xAI Responses stream frame against TS
// xaiResponsesChunkSchema.
func ValidateXAIResponsesChunk(data []byte) error {
	var value interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	m, ok := value.(map[string]interface{})
	if !ok {
		return zBad("object", value, true, nil)
	}
	t, _ := m["type"].(string)
	node, known := zXaiChunk[t]
	if !known {
		return &ChunkSchemaError{Path: []interface{}{"type"}, Code: "invalid_union", Message: "Invalid input"}
	}
	if bad := node(value, true, nil); bad != nil {
		return bad
	}
	return nil
}

// CheckXAIResponsesChunk validates data and returns the error-part message and
// typed error on failure ("", nil when the frame is accepted).
func CheckXAIResponsesChunk(data string) (string, error) {
	err := ValidateXAIResponsesChunk([]byte(data))
	if err == nil {
		return "", nil
	}
	if _, ok := err.(*ChunkSchemaError); !ok {
		err = nil
	}
	return ChunkParseFailure(data, err)
}

// XAIResponsesResponseValidator is ChatResponseValidator for the xAI
// Responses API (xaiResponsesResponseSchema).
func XAIResponsesResponseValidator(providerName string) func(statusCode int, headers http.Header, body []byte) error {
	return responseValidator(providerName, func(body []byte) error {
		var value interface{}
		if err := json.Unmarshal(body, &value); err != nil {
			return err
		}
		if bad := zXaiResponseFields(false)(value, true, nil); bad != nil {
			return bad
		}
		return nil
	})
}

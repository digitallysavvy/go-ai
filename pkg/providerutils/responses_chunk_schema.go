package providerutils

import "encoding/json"

// Port of openaiResponsesChunkSchema (openai/src/responses/openai-responses-api.ts):
// a union of one object schema per modeled event type, the two error shapes,
// and a catch-all `{type: string}.loose()` that only accepts a frame whose
// type (or, for response.output_item.*, whose item type) is NOT modeled, so a
// malformed modeled frame is a parse failure while an unknown event passes.
// Combinators live in chunk_schema.go.

var (
	zRespOptStr = zOptional(zStr)

	zRespUsage = zObj(
		fl("input_tokens", zNum),
		fl("input_tokens_details", zNullish(zObj(
			fl("cached_tokens", nNum), fl("cache_write_tokens", nNum),
			fl("orchestration_input_tokens", nNum), fl("orchestration_input_cached_tokens", nNum)))),
		fl("output_tokens", zNum),
		fl("output_tokens_details", zNullish(zObj(fl("reasoning_tokens", nNum), fl("orchestration_output_tokens", nNum)))),
		fl("total_tokens", zOptional(zNum)),
	)

	zRespKeys = zNullish(zArr(zStr))

	zRespComputerAction = zDiscType(map[string]zNode{
		"click":        zObj(fl("type", zLit("click")), fl("button", zEnum("left", "right", "wheel", "back", "forward")), fl("x", zNum), fl("y", zNum), fl("keys", zRespKeys)),
		"double_click": zObj(fl("type", zLit("double_click")), fl("x", zNum), fl("y", zNum), fl("keys", zRespKeys)),
		"drag":         zObj(fl("type", zLit("drag")), fl("path", zArr(zObj(fl("x", zNum), fl("y", zNum)))), fl("keys", zRespKeys)),
		"keypress":     zObj(fl("type", zLit("keypress")), fl("keys", zArr(zStr))),
		"move":         zObj(fl("type", zLit("move")), fl("x", zNum), fl("y", zNum), fl("keys", zRespKeys)),
		"screenshot":   zObj(fl("type", zLit("screenshot"))),
		"scroll":       zObj(fl("type", zLit("scroll")), fl("x", zNum), fl("y", zNum), fl("scroll_x", zNum), fl("scroll_y", zNum), fl("keys", zRespKeys)),
		"type":         zObj(fl("type", zLit("type")), fl("text", zStr)),
		"wait":         zObj(fl("type", zLit("wait"))),
	})

	zRespComputerCall = zObj(
		fl("type", zLit("computer_call")), fl("id", zStr), fl("call_id", nStr),
		fl("status", zEnum("in_progress", "completed", "incomplete")),
		fl("action", zNullish(zRespComputerAction)),
		fl("actions", zNullish(zArr(zRespComputerAction))),
		fl("pending_safety_checks", zNullish(zArr(zObj(fl("id", zStr), fl("code", nStr), fl("message", nStr))))),
	)

	zRespCaller = zDiscType(map[string]zNode{
		"direct":  zObj(fl("type", zLit("direct"))),
		"program": zObj(fl("type", zLit("program")), fl("caller_id", zStr)),
	})

	zRespProgram       = zObj(fl("type", zLit("program")), fl("id", zStr), fl("call_id", zStr), fl("code", zStr), fl("fingerprint", zStr))
	zRespProgramOutput = zObj(fl("type", zLit("program_output")), fl("id", zStr), fl("call_id", zStr), fl("result", zStr), fl("status", zEnum("completed", "incomplete")))

	zRespLocalShell = zObj(fl("type", zLit("local_shell_call")), fl("id", zStr), fl("call_id", zStr), fl("action", zObj(
		fl("type", zLit("exec")), fl("command", zArr(zStr)), fl("timeout_ms", zOptional(zNum)), fl("user", zRespOptStr),
		fl("working_directory", zRespOptStr), fl("env", zOptional(zRec(zStr))))))

	zRespPhase = zNullish(zEnum("commentary", "final_answer"))

	zRespCodeOutputs = zNullable(zArr(zDiscType(map[string]zNode{
		"logs":  zObj(fl("type", zLit("logs")), fl("logs", zStr)),
		"image": zObj(fl("type", zLit("image")), fl("url", zStr)),
	})))

	zRespApplyPatch = zObj(
		fl("type", zLit("apply_patch_call")), fl("id", zStr), fl("call_id", zStr),
		fl("status", zEnum("in_progress", "completed")),
		fl("operation", zDiscType(map[string]zNode{
			"create_file": zObj(fl("type", zLit("create_file")), fl("path", zStr), fl("diff", zStr)),
			"delete_file": zObj(fl("type", zLit("delete_file")), fl("path", zStr)),
			"update_file": zObj(fl("type", zLit("update_file")), fl("path", zStr), fl("diff", zStr)),
		})),
	)

	zRespStatus3 = zEnum("in_progress", "completed", "incomplete")

	zRespShellCall = zObj(fl("type", zLit("shell_call")), fl("id", zStr), fl("call_id", zStr), fl("status", zRespStatus3),
		fl("action", zObj(fl("commands", zArr(zStr)))))

	zRespShellOutput = zObj(fl("type", zLit("shell_call_output")), fl("id", zStr), fl("call_id", zStr), fl("status", zRespStatus3),
		fl("output", zArr(zObj(fl("stdout", zStr), fl("stderr", zStr), fl("outcome", zDiscType(map[string]zNode{
			"timeout": zObj(fl("type", zLit("timeout"))),
			"exit":    zObj(fl("type", zLit("exit")), fl("exit_code", zNum)),
		}))))))

	zRespExecution = zEnum("server", "client")

	zRespToolSearchCall = zObj(fl("type", zLit("tool_search_call")), fl("id", zStr), fl("execution", zRespExecution),
		fl("call_id", zNullable(zStr)), fl("status", zRespStatus3), fl("arguments", zAny))

	zRespToolSearchOutput = zObj(fl("type", zLit("tool_search_output")), fl("id", zStr), fl("execution", zRespExecution),
		fl("call_id", zNullable(zStr)), fl("status", zRespStatus3), fl("tools", zArr(zRec(zAny))))

	zRespMcpError = zUnionOf(zStr, zObj(fl("type", zRespOptStr), fl("code", zOptional(zUnionOf(zNum, zStr))), fl("message", zRespOptStr)))

	zRespItemAdded = zDiscType(map[string]zNode{
		"message":   zObj(fl("type", zLit("message")), fl("id", zStr), fl("phase", zRespPhase)),
		"reasoning": zObj(fl("type", zLit("reasoning")), fl("id", zStr), fl("encrypted_content", nStr)),
		"function_call": zObj(fl("type", zLit("function_call")), fl("id", zStr), fl("call_id", zStr), fl("name", zStr), fl("arguments", zStr),
			fl("async", zNullish(zBoolean)), fl("namespace", nStr), fl("caller", zNullish(zRespCaller))),
		"program":               zRespProgram,
		"program_output":        zRespProgramOutput,
		"web_search_call":       zObj(fl("type", zLit("web_search_call")), fl("id", zStr), fl("status", zStr)),
		"computer_call":         zRespComputerCall,
		"file_search_call":      zObj(fl("type", zLit("file_search_call")), fl("id", zStr)),
		"local_shell_call":      zRespLocalShell,
		"image_generation_call": zObj(fl("type", zLit("image_generation_call")), fl("id", zStr)),
		"code_interpreter_call": zObj(fl("type", zLit("code_interpreter_call")), fl("id", zStr), fl("container_id", zStr), fl("code", zNullable(zStr)),
			fl("outputs", zRespCodeOutputs), fl("status", zStr)),
		"mcp_call":             zObj(fl("type", zLit("mcp_call")), fl("id", zStr), fl("status", zStr), fl("approval_request_id", nStr)),
		"mcp_list_tools":       zObj(fl("type", zLit("mcp_list_tools")), fl("id", zStr)),
		"mcp_approval_request": zObj(fl("type", zLit("mcp_approval_request")), fl("id", zStr)),
		"apply_patch_call":     zRespApplyPatch,
		"custom_tool_call": zObj(fl("type", zLit("custom_tool_call")), fl("id", zStr), fl("call_id", zStr), fl("name", zStr), fl("input", zStr),
			fl("async", zNullish(zBoolean))),
		"shell_call":         zRespShellCall,
		"compaction":         zObj(fl("type", zLit("compaction")), fl("id", zStr), fl("encrypted_content", nStr)),
		"shell_call_output":  zRespShellOutput,
		"tool_search_call":   zRespToolSearchCall,
		"tool_search_output": zRespToolSearchOutput,
	})

	zRespWebSearchAction = zNullish(zDiscType(map[string]zNode{
		"search": zObj(fl("type", zLit("search")), fl("query", nStr), fl("queries", zNullish(zArr(zStr))),
			fl("sources", zNullish(zArr(zDiscType(map[string]zNode{
				"url": zObj(fl("type", zLit("url")), fl("url", zStr)),
				"api": zObj(fl("type", zLit("api")), fl("name", zStr)),
			}))))),
		"open_page":    zObj(fl("type", zLit("open_page")), fl("url", nStr)),
		"find_in_page": zObj(fl("type", zLit("find_in_page")), fl("url", nStr), fl("pattern", nStr)),
	}))

	zRespItemDone = zDiscType(map[string]zNode{
		"message":   zObj(fl("type", zLit("message")), fl("id", zStr), fl("phase", zRespPhase)),
		"reasoning": zObj(fl("type", zLit("reasoning")), fl("id", zStr), fl("encrypted_content", nStr)),
		"function_call": zObj(fl("type", zLit("function_call")), fl("id", zStr), fl("call_id", zStr), fl("name", zStr), fl("arguments", zStr),
			fl("async", zNullish(zBoolean)), fl("status", zRespStatus3), fl("namespace", nStr), fl("caller", zNullish(zRespCaller))),
		"program":        zRespProgram,
		"program_output": zRespProgramOutput,
		"custom_tool_call": zObj(fl("type", zLit("custom_tool_call")), fl("id", zStr), fl("call_id", zStr), fl("name", zStr), fl("input", zStr),
			fl("async", zNullish(zBoolean)), fl("status", zLit("completed"))),
		"code_interpreter_call": zObj(fl("type", zLit("code_interpreter_call")), fl("id", zStr), fl("code", zNullable(zStr)),
			fl("container_id", zStr), fl("outputs", zRespCodeOutputs)),
		"image_generation_call": zObj(fl("type", zLit("image_generation_call")), fl("id", zStr), fl("result", zStr)),
		"web_search_call":       zObj(fl("type", zLit("web_search_call")), fl("id", zStr), fl("status", zStr), fl("action", zRespWebSearchAction)),
		"file_search_call": zObj(fl("type", zLit("file_search_call")), fl("id", zStr), fl("queries", zArr(zStr)),
			fl("results", zNullish(zArr(zObj(
				fl("attributes", zRec(zUnionOf(zStr, zNum, zBoolean))), fl("file_id", zStr), fl("filename", zStr), fl("score", zNum), fl("text", zStr)))))),
		"local_shell_call": zRespLocalShell,
		"computer_call":    zRespComputerCall,
		"mcp_call": zObj(fl("type", zLit("mcp_call")), fl("id", zStr), fl("status", zStr), fl("arguments", zStr), fl("name", zStr),
			fl("server_label", zStr), fl("output", nStr), fl("error", zNullish(zRespMcpError)), fl("approval_request_id", nStr)),
		"mcp_list_tools": zObj(fl("type", zLit("mcp_list_tools")), fl("id", zStr), fl("server_label", zStr),
			fl("tools", zArr(zObj(fl("name", zStr), fl("description", zRespOptStr), fl("input_schema", zAny), fl("annotations", zOptional(zRec(zAny)))))),
			fl("error", zOptional(zRespMcpError))),
		"mcp_approval_request": zObj(fl("type", zLit("mcp_approval_request")), fl("id", zStr), fl("server_label", zStr), fl("name", zStr),
			fl("arguments", zStr), fl("approval_request_id", zRespOptStr)),
		"apply_patch_call":   zRespApplyPatch,
		"shell_call":         zRespShellCall,
		"compaction":         zObj(fl("type", zLit("compaction")), fl("id", zStr), fl("encrypted_content", zStr)),
		"shell_call_output":  zRespShellOutput,
		"tool_search_call":   zRespToolSearchCall,
		"tool_search_output": zRespToolSearchOutput,
	})

	zRespCommonResponse = []zFld{
		fl("incomplete_details", zNullish(zObj(fl("reason", zStr)))),
		fl("usage", zNullish(zRespUsage)),
		fl("reasoning", zNullish(zObj(fl("context", nStr)))),
		fl("service_tier", nStr),
	}

	zRespCreated = zObj(fl("id", zStr), fl("created_at", zNum), fl("model", zStr), fl("service_tier", nStr))

	zRespItemDelta = func(typ string) zNode {
		return zObj(fl("type", zLit(typ)), fl("item_id", zStr), fl("output_index", zNum), fl("delta", zStr))
	}

	zRespAnnotation = zDiscType(map[string]zNode{
		"url_citation":            zObj(fl("type", zLit("url_citation")), fl("start_index", zNum), fl("end_index", zNum), fl("url", zStr), fl("title", zStr)),
		"file_citation":           zObj(fl("type", zLit("file_citation")), fl("file_id", zStr), fl("filename", zStr), fl("index", zNum)),
		"container_file_citation": zObj(fl("type", zLit("container_file_citation")), fl("container_id", zStr), fl("file_id", zStr), fl("filename", zStr), fl("start_index", zNum), fl("end_index", zNum)),
		"file_path":               zObj(fl("type", zLit("file_path")), fl("file_id", zStr), fl("index", zNum)),
	})

	// zRespModeled maps each modeled event type to its schema.
	zRespModeled = map[string]zNode{
		"response.output_text.delta": zObj(fl("type", zLit("response.output_text.delta")), fl("item_id", zStr), fl("output_index", nNum), fl("delta", zStr),
			fl("logprobs", zNullish(zArr(zObj(fl("token", zStr), fl("logprob", zNum), fl("top_logprobs", zArr(zObj(fl("token", zStr), fl("logprob", zNum))))))))),
		"response.completed":  zObj(fl("type", zEnum("response.completed", "response.incomplete")), fl("response", zObj(zRespCommonResponse...))),
		"response.incomplete": zObj(fl("type", zEnum("response.completed", "response.incomplete")), fl("response", zObj(zRespCommonResponse...))),
		"response.failed": zObj(fl("type", zLit("response.failed")), fl("sequence_number", zNum), fl("response", zObj(append([]zFld{
			fl("error", zNullish(zObj(fl("code", nStr), fl("message", zStr))))}, zRespCommonResponse...)...))),
		"response.created":                       zObj(fl("type", zLit("response.created")), fl("response", zRespCreated)),
		"response.in_progress":                   zObj(fl("type", zLit("response.in_progress")), fl("response", zRespCreated)),
		"response.output_item.added":             zObj(fl("type", zLit("response.output_item.added")), fl("output_index", zNum), fl("item", zRespItemAdded)),
		"response.output_item.done":              zObj(fl("type", zLit("response.output_item.done")), fl("output_index", zNum), fl("item", zRespItemDone)),
		"response.function_call_arguments.delta": zRespItemDelta("response.function_call_arguments.delta"),
		"response.function_call_arguments.done": zObj(fl("type", zLit("response.function_call_arguments.done")), fl("item_id", zStr),
			fl("output_index", zNum), fl("arguments", zStr)),
		"response.custom_tool_call_input.delta": zRespItemDelta("response.custom_tool_call_input.delta"),
		"response.image_generation_call.partial_image": zObj(fl("type", zLit("response.image_generation_call.partial_image")), fl("item_id", zStr),
			fl("output_index", zNum), fl("partial_image_b64", zStr)),
		"response.code_interpreter_call_code.delta": zRespItemDelta("response.code_interpreter_call_code.delta"),
		"response.code_interpreter_call_code.done": zObj(fl("type", zLit("response.code_interpreter_call_code.done")), fl("item_id", zStr),
			fl("output_index", zNum), fl("code", zStr)),
		"response.output_text.annotation.added": zObj(fl("type", zLit("response.output_text.annotation.added")), fl("annotation", zRespAnnotation)),
		"response.reasoning_summary_part.added": zObj(fl("type", zLit("response.reasoning_summary_part.added")), fl("item_id", zStr),
			fl("output_index", nNum), fl("summary_index", zNum)),
		"response.reasoning_summary_text.delta": zObj(fl("type", zLit("response.reasoning_summary_text.delta")), fl("item_id", zStr),
			fl("output_index", nNum), fl("summary_index", zNum), fl("delta", zStr)),
		"response.reasoning_summary_part.done": zObj(fl("type", zLit("response.reasoning_summary_part.done")), fl("item_id", zStr),
			fl("output_index", nNum), fl("summary_index", zNum)),
		"response.apply_patch_call_operation_diff.delta": zObj(fl("type", zLit("response.apply_patch_call_operation_diff.delta")), fl("item_id", zStr),
			fl("output_index", zNum), fl("delta", zStr), fl("obfuscation", nStr)),
		"response.apply_patch_call_operation_diff.done": zObj(fl("type", zLit("response.apply_patch_call_operation_diff.done")), fl("item_id", zStr),
			fl("output_index", zNum), fl("diff", zStr)),
	}

	zRespErrNested = zObj(fl("type", zLit("error")), fl("sequence_number", zNum), fl("error", zObj(
		fl("type", zStr), fl("code", nStr), fl("message", zStr), fl("param", nStr))))
	zRespErrFlat = zObj(fl("type", zLit("error")), fl("sequence_number", zNum), fl("code", nStr), fl("message", zStr), fl("param", nStr))

	zRespModeledItemTypes = map[string]bool{
		"apply_patch_call": true, "code_interpreter_call": true, "compaction": true, "computer_call": true, "custom_tool_call": true,
		"file_search_call": true, "function_call": true, "image_generation_call": true, "local_shell_call": true, "mcp_approval_request": true,
		"mcp_call": true, "mcp_list_tools": true, "message": true, "program": true, "program_output": true, "reasoning": true,
		"shell_call": true, "shell_call_output": true, "tool_search_call": true, "tool_search_output": true, "web_search_call": true,
	}
)

func zBoolean(v interface{}, present bool, path []interface{}) *ChunkSchemaError {
	if _, ok := v.(bool); !ok {
		return zBad("boolean", v, present, path)
	}
	return nil
}

// isModeledResponsesChunk mirrors isModeledOpenAIResponsesChunk.
func isModeledResponsesChunk(m map[string]interface{}) bool {
	t, ok := m["type"].(string)
	if !ok {
		return false
	}
	if _, modeled := zRespModeled[t]; !modeled && t != "error" {
		return false
	}
	if t != "response.output_item.added" && t != "response.output_item.done" {
		return true
	}
	item, ok := m["item"].(map[string]interface{})
	if !ok {
		return true
	}
	it, ok := item["type"].(string)
	if !ok {
		return true
	}
	return zRespModeledItemTypes[it]
}

// ValidateResponsesChunk checks a Responses API stream frame against TS
// openaiResponsesChunkSchema. nil means TS's safeParse succeeds (including the
// unknown_chunk fallback for event types the schema does not model).
func ValidateResponsesChunk(data []byte) error {
	var value interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	m, isObj := value.(map[string]interface{})
	if !isObj {
		return zBad("object", value, true, nil)
	}
	if _, ok := m["type"].(string); !ok {
		return &ChunkSchemaError{path: []interface{}{"type"}, code: "invalid_type", message: "Invalid input: expected string, received " + zType(m["type"], m["type"] != nil || hasKey(m, "type"))}
	}
	if !isModeledResponsesChunk(m) {
		return nil
	}
	t := m["type"].(string)
	if t == "error" {
		if zRespErrNested(value, true, nil) == nil || zRespErrFlat(value, true, nil) == nil {
			return nil
		}
		return &ChunkSchemaError{code: "custom", message: "Known response chunk failed schema validation"}
	}
	if bad := zRespModeled[t](value, true, nil); bad != nil {
		return bad
	}
	return nil
}

func hasKey(m map[string]interface{}, k string) bool { _, ok := m[k]; return ok }

// CheckResponsesChunk validates data and returns the error-part message and
// typed error on failure ("", nil when the frame is accepted).
func CheckResponsesChunk(data string) (string, error) {
	err := ValidateResponsesChunk([]byte(data))
	if err == nil {
		return "", nil
	}
	if _, ok := err.(*ChunkSchemaError); !ok {
		err = nil
	}
	return ChunkParseFailure(data, err)
}

package providerutils

import (
	"encoding/json"
	"net/http"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// This file ports the zod response schemas the TS OpenAI-family providers
// apply to every non-streaming body through createJsonResponseHandler
// (openaiChatResponseSchema, OpenAICompatibleChatResponseSchema,
// openaiCompletionResponseSchema, groqChatResponseSchema,
// mistralChatResponseSchema, deepseekChatResponseSchema,
// moonshotAIChatResponseSchema, alibabaChatResponseSchema). A body that does
// not match is an APICallError "Invalid JSON response" whose cause is the
// TypeValidationError (or the JSONParseError for text that is not JSON).

// ChatResponseKind selects the TS response schema of a non-streaming call.
// The zero value is the openai-compatible OpenAICompatibleChatResponseSchema.
type ChatResponseKind int

const (
	// ResponseKindOpenAICompatible is OpenAICompatibleChatResponseSchema (also
	// the Go OpenAI chat model in openai-compatible mode).
	ResponseKindOpenAICompatible ChatResponseKind = iota
	// ResponseKindOpenAI is openaiChatResponseSchema (also azure).
	ResponseKindOpenAI
	// ResponseKindOpenAICompletion is openaiCompletionResponseSchema.
	ResponseKindOpenAICompletion
	// ResponseKindCompatCompletion is openaiCompatibleCompletionResponseSchema.
	ResponseKindCompatCompletion
	ResponseKindGroq
	ResponseKindDeepSeek
	ResponseKindMoonshot
	ResponseKindMistral
	ResponseKindAlibaba
	// ResponseKindCohere is cohereChatResponseSchema.
	ResponseKindCohere
)

var (
	zOpenAIUsage = zNullish(zObj(
		fl("prompt_tokens", nNum), fl("completion_tokens", nNum), fl("total_tokens", nNum),
		fl("prompt_tokens_details", zNullish(zObj(fl("cached_tokens", nNum), fl("cache_write_tokens", nNum)))),
		fl("completion_tokens_details", zNullish(zObj(
			fl("reasoning_tokens", nNum), fl("accepted_prediction_tokens", nNum), fl("rejected_prediction_tokens", nNum)))),
	))

	zOpenAIChatResponse = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("choices", zArr(zObj(
			fl("message", zObj(
				fl("role", zNullish(zLit("assistant"))),
				fl("content", nStr),
				fl("audio", zNullish(zObj(fl("transcript", nStr)))),
				fl("tool_calls", zNullish(zArr(zObj(
					fl("id", nStr), fl("type", zLit("function")),
					fl("function", zObj(fl("name", zStr), fl("arguments", zStr))),
				)))),
				fl("annotations", zNullish(zArr(zObj(
					fl("type", zLit("url_citation")),
					fl("url_citation", zObj(fl("start_index", zNum), fl("end_index", zNum), fl("url", zStr), fl("title", zStr))),
				)))),
			)),
			fl("index", zNum),
			fl("logprobs", zNullish(zObj(fl("content", zNullish(zArr(zObj(
				fl("token", zStr), fl("logprob", zNum),
				fl("top_logprobs", zArr(zObj(fl("token", zStr), fl("logprob", zNum)))),
			))))))),
			fl("finish_reason", nStr),
		))),
		fl("usage", zOpenAIUsage),
	)

	zCompatChatResponse = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("choices", zArr(zObj(
			fl("message", zObj(
				fl("role", zNullish(zLit("assistant"))),
				fl("content", zCompatContent),
				fl("reasoning_content", nStr), fl("reasoning", nStr),
				fl("tool_calls", zNullish(zArr(zObj(
					fl("id", nStr),
					fl("function", zObj(fl("name", zStr), fl("arguments", zStr))),
					fl("extra_content", zNullish(zObj(fl("google", zNullish(zObj(fl("thought_signature", nStr))))))),
				)))),
			)),
			fl("finish_reason", nStr),
		))),
		fl("usage", zCompatUsage),
	)

	zCompletionResponse = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("choices", zArr(zObj(
			fl("text", zStr), fl("finish_reason", zStr),
			fl("logprobs", zNullish(zObj(
				fl("tokens", zArr(zStr)), fl("token_logprobs", zArr(zNum)),
				fl("top_logprobs", zNullish(zArr(zRec(zNum)))),
			))),
		))),
		fl("usage", zNullish(zObj(fl("prompt_tokens", zNum), fl("completion_tokens", zNum), fl("total_tokens", zNum)))),
	)

	zCompatCompletionResponse = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("choices", zArr(zObj(fl("text", zStr), fl("finish_reason", zStr)))),
		fl("usage", zNullish(zObj(fl("prompt_tokens", zNum), fl("completion_tokens", zNum), fl("total_tokens", zNum)))),
	)

	zGroqResponse = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("choices", zArr(zObj(
			fl("message", zObj(
				fl("content", nStr), fl("reasoning", nStr),
				fl("tool_calls", zNullish(zArr(zObj(
					fl("id", nStr), fl("type", zLit("function")),
					fl("function", zObj(fl("name", zStr), fl("arguments", zStr))),
				)))),
			)),
			fl("index", zNum),
			fl("finish_reason", nStr),
		))),
		fl("usage", zNullish(zObj(
			fl("prompt_tokens", nNum), fl("completion_tokens", nNum), fl("total_tokens", nNum),
			fl("prompt_tokens_details", zNullish(zObj(fl("cached_tokens", nNum)))),
			fl("completion_tokens_details", zNullish(zObj(fl("reasoning_tokens", nNum)))),
		))),
	)

	zDeepSeekResponse = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("object", zNullish(zLit("chat.completion"))), fl("system_fingerprint", nStr),
		fl("choices", zArr(zObj(
			fl("index", nNum),
			fl("message", zObj(
				fl("role", zNullish(zLit("assistant"))),
				fl("content", nStr), fl("reasoning_content", nStr),
				fl("tool_calls", zNullish(zArr(zObj(
					fl("id", nStr), fl("type", zNullish(zLit("function"))),
					fl("function", zObj(fl("name", zStr), fl("arguments", zStr))),
				)))),
			)),
			fl("logprobs", zNullish(zObj(
				fl("content", zNullish(zArr(zLogprobEl))),
				fl("reasoning_content", zNullish(zArr(zLogprobEl))),
			))),
			fl("finish_reason", nStr),
		))),
		fl("usage", zDeepSeekUsage),
	)

	zMoonshotResponse = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("object", zNullish(zLit("chat.completion"))),
		fl("choices", zArr(zObj(
			fl("index", nNum),
			fl("message", zObj(
				fl("role", zNullish(zLit("assistant"))),
				fl("content", nStr), fl("reasoning_content", nStr),
				fl("tool_calls", zNullish(zArr(zObj(
					fl("id", nStr), fl("type", zNullish(zLit("function"))),
					fl("function", zObj(fl("name", zStr), fl("arguments", zStr))),
				)))),
			)),
			fl("logprobs", zNullish(zObj(fl("content", zNullish(zArr(zLogprobEl)))))),
			fl("finish_reason", nStr),
		))),
		fl("usage", zMoonshotUsage),
	)

	zMistralResponse = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("choices", zArr(zObj(
			fl("message", zObj(
				fl("role", zLit("assistant")),
				fl("content", zMistralContent),
				fl("tool_calls", zNullish(zArr(zObj(
					fl("id", zStr),
					fl("function", zObj(fl("name", zStr), fl("arguments", zStr))),
				)))),
			)),
			fl("index", zNum),
			fl("finish_reason", nStr),
		))),
		fl("object", zLit("chat.completion")),
		// mistralUsageSchema is required in the response (nullish only in chunks).
		fl("usage", zObj(
			fl("prompt_tokens", zNum), fl("completion_tokens", zNum), fl("total_tokens", zNum),
			fl("prompt_audio_seconds", nNum), fl("request_count", nNum), fl("service_tier", nStr), fl("num_cached_tokens", nNum),
			fl("prompt_tokens_details", zNullish(zMistralDetails)),
			fl("prompt_token_details", zNullish(zMistralDetails)),
			fl("completion_tokens_details", zNullish(zObj(fl("reasoning_tokens", nNum)))),
		)),
	)

	zCohereTokens = zObj(fl("input_tokens", zNum), fl("output_tokens", zNum))

	zCohereResponse = zObj(
		fl("generation_id", nStr),
		fl("message", zObj(
			fl("role", zStr),
			fl("content", zNullish(zArr(zUnionOf(
				zObj(fl("type", zLit("text")), fl("text", zStr)),
				zObj(fl("type", zLit("thinking")), fl("thinking", zStr)),
			)))),
			fl("tool_plan", nStr),
			fl("tool_calls", zNullish(zArr(zObj(
				fl("id", zStr), fl("type", zLit("function")),
				fl("function", zObj(fl("name", zStr), fl("arguments", zStr))),
			)))),
			fl("citations", zNullish(zArr(zObj(
				fl("start", zNum), fl("end", zNum), fl("text", zStr),
				fl("sources", zArr(zObj(
					fl("type", zOptional(zStr)), fl("id", zOptional(zStr)),
					fl("document", zObj(fl("id", zOptional(zStr)), fl("text", zStr), fl("title", zStr))),
				))),
				fl("type", zOptional(zStr)),
			)))),
		)),
		fl("finish_reason", zStr),
		fl("usage", zObj(
			fl("billed_units", zNullish(zCohereTokens)),
			fl("tokens", zCohereTokens),
			fl("cached_tokens", nNum),
		)),
	)

	zAlibabaResponse = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("choices", zArr(zObj(
			fl("message", zObj(
				fl("role", zNullish(zLit("assistant"))),
				fl("content", nStr), fl("reasoning_content", nStr),
				fl("tool_calls", zNullish(zArr(zObj(
					fl("id", zStr), fl("type", zLit("function")),
					fl("function", zObj(fl("name", zStr), fl("arguments", zStr))),
				)))),
			)),
			fl("finish_reason", nStr),
			fl("index", zNum),
		))),
		fl("usage", zNullish(zObj(
			fl("prompt_tokens", zNum), fl("completion_tokens", zNum), fl("total_tokens", zNum),
			fl("prompt_tokens_details", zNullish(zObj(fl("cached_tokens", nNum), fl("cache_creation_input_tokens", nNum), fl("cache_type", nStr)))),
			fl("completion_tokens_details", zNullish(zObj(fl("reasoning_tokens", nNum)))),
		))),
	)
)

func (k ChatResponseKind) node() zNode {
	switch k {
	case ResponseKindOpenAI:
		return zOpenAIChatResponse
	case ResponseKindOpenAICompletion:
		return zCompletionResponse
	case ResponseKindCompatCompletion:
		return zCompatCompletionResponse
	case ResponseKindGroq:
		return zGroqResponse
	case ResponseKindDeepSeek:
		return zDeepSeekResponse
	case ResponseKindMoonshot:
		return zMoonshotResponse
	case ResponseKindMistral:
		return zMistralResponse
	case ResponseKindAlibaba:
		return zAlibabaResponse
	case ResponseKindCohere:
		return zCohereResponse
	}
	return zCompatChatResponse
}

// ValidateChatResponse checks a non-streaming response body against the TS
// zod response schema of kind. A nil result means TS's safeParse succeeds. The
// error is a *ChunkSchemaError, or the JSON syntax error when body is not
// valid JSON.
func ValidateChatResponse(body []byte, kind ChatResponseKind) error {
	var value interface{}
	if err := json.Unmarshal(body, &value); err != nil {
		return err
	}
	if bad := kind.node()(value, true, nil); bad != nil {
		return bad
	}
	return nil
}

// ChatResponseValidator returns the response check of an http.Request
// (ValidateResponse): TS createJsonResponseHandler(schema) turns a body that
// fails the schema into an APICallError "Invalid JSON response" with the
// status, headers and body of the response and the TypeValidationError (or
// JSONParseError) as cause.
func ChatResponseValidator(providerName string, kind ChatResponseKind) func(statusCode int, headers http.Header, body []byte) error {
	return responseValidator(providerName, func(body []byte) error { return ValidateChatResponse(body, kind) })
}

func responseValidator(providerName string, validate func(body []byte) error) func(statusCode int, headers http.Header, body []byte) error {
	return func(statusCode int, headers http.Header, body []byte) error {
		err := validate(body)
		if err == nil {
			return nil
		}
		return InvalidJSONResponseError(providerName, statusCode, headers, body, err)
	}
}

// InvalidJSONResponseError builds the failure of TS createJsonResponseHandler:
// an APICallError "Invalid JSON response" carrying the response status,
// headers and body, whose cause is the TypeValidationError for a schema
// mismatch (cause is a *ChunkSchemaError) or the JSONParseError for text that
// is not JSON (any other cause).
func InvalidJSONResponseError(providerName string, statusCode int, headers http.Header, body []byte, cause error) error {
	if _, ok := cause.(*ChunkSchemaError); !ok {
		cause = nil // not JSON: ChunkParseFailure reports the parse failure
	}
	_, typed := ChunkParseFailure(string(body), cause)
	return &providererrors.ProviderError{
		Provider:        providerName,
		StatusCode:      statusCode,
		Message:         InvalidJSONResponseMessage,
		Cause:           typed,
		ResponseHeaders: ExtractHeaders(headers),
		ResponseBody:    string(body),
	}
}

// InvalidJSONResponseMessage is the TS createJsonResponseHandler failure
// message.
const InvalidJSONResponseMessage = "Invalid JSON response"

// IsInvalidJSONResponse reports whether err is the createJsonResponseHandler
// failure (so provider error handlers can return it as is).
func IsInvalidJSONResponse(err error) bool {
	for err != nil {
		if pe, ok := err.(*providererrors.ProviderError); ok && pe.Message == InvalidJSONResponseMessage {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

var (
	zRespAssistantMessage = zObj(
		fl("type", zLit("message")), fl("role", zLit("assistant")), fl("id", zStr), fl("phase", zRespPhase),
		fl("content", zArr(zObj(
			fl("type", zLit("output_text")), fl("text", zStr),
			fl("logprobs", zNullish(zArr(zObj(fl("token", zStr), fl("logprob", zNum),
				fl("top_logprobs", zArr(zObj(fl("token", zStr), fl("logprob", zNum)))))))),
			fl("annotations", zArr(zRespAnnotation)),
		))),
	)

	// openaiResponsesResponseSchema output items: the output_item.done
	// members, except message/reasoning (full bodies), and function_call and
	// custom_tool_call (no status).
	zRespOutputItem = func() zNode {
		items := map[string]zNode{
			"message": zRespAssistantMessage,
			"reasoning": zObj(fl("type", zLit("reasoning")), fl("id", zStr), fl("encrypted_content", nStr),
				fl("summary", zArr(zObj(fl("type", zLit("summary_text")), fl("text", zStr))))),
			"function_call": zObj(fl("type", zLit("function_call")), fl("call_id", zStr), fl("name", zStr), fl("arguments", zStr), fl("id", zStr),
				fl("async", zNullish(zBoolean)), fl("namespace", nStr), fl("caller", zNullish(zRespCaller))),
			"custom_tool_call": zObj(fl("type", zLit("custom_tool_call")), fl("call_id", zStr), fl("name", zStr), fl("input", zStr), fl("id", zStr),
				fl("async", zNullish(zBoolean))),
		}
		for k, v := range zRespItemDoneMembers {
			if _, replaced := items[k]; !replaced {
				items[k] = v
			}
		}
		return zDiscType(items)
	}()

	zOpenAIResponsesResponse = zObj(
		fl("id", zRespOptStr), fl("created_at", zOptional(zNum)),
		fl("error", zNullish(zObj(fl("message", zStr), fl("type", zStr), fl("param", nStr), fl("code", zStr)))),
		fl("model", zRespOptStr),
		fl("output", zOptional(zArr(zRespOutputItem))),
		fl("service_tier", nStr),
		fl("reasoning", zNullish(zObj(fl("context", nStr)))),
		fl("incomplete_details", zNullish(zObj(fl("reason", zStr)))),
		fl("usage", zNullish(zRespUsage)),
	)
)

// ResponsesResponseValidator is ChatResponseValidator for the OpenAI
// Responses API (openaiResponsesResponseSchema, also Azure).
func ResponsesResponseValidator(providerName string) func(statusCode int, headers http.Header, body []byte) error {
	return responseValidator(providerName, func(body []byte) error {
		var value interface{}
		if err := json.Unmarshal(body, &value); err != nil {
			return err
		}
		if bad := zOpenAIResponsesResponse(value, true, nil); bad != nil {
			return bad
		}
		return nil
	})
}

var zHuggingFaceResponse = zObj(
	fl("id", zStr), fl("model", zStr), fl("object", zStr), fl("created_at", zNum), fl("status", zStr),
	fl("error", zNullable(zAny)), fl("instructions", zNullable(zAny)), fl("max_output_tokens", zNullable(zAny)),
	fl("metadata", zNullable(zAny)), fl("tool_choice", zAny),
	fl("tools", zArr(zAny)), fl("temperature", zNum), fl("top_p", zNum),
	fl("incomplete_details", zOptional(zNullable(zObj(fl("reason", zStr))))),
	fl("usage", zOptional(zNullable(zObj(
		fl("input_tokens", zNum),
		fl("input_tokens_details", zOptional(zObj(fl("cached_tokens", zNum)))),
		fl("output_tokens", zNum),
		fl("output_tokens_details", zOptional(zObj(fl("reasoning_tokens", zNum)))),
		fl("total_tokens", zNum),
	)))),
	fl("output", zArr(zDiscType(map[string]zNode{
		"message": zObj(fl("type", zLit("message")), fl("id", zStr), fl("role", zOptional(zStr)), fl("status", zOptional(zStr)),
			fl("content", zArr(zObj(fl("type", zLit("output_text")), fl("text", zStr), fl("annotations", zOptional(zArr(zAny))))))),
		"reasoning": zObj(fl("type", zLit("reasoning")), fl("id", zStr), fl("status", zOptional(zStr)),
			fl("content", zArr(zObj(fl("type", zLit("reasoning_text")), fl("text", zStr)))),
			fl("summary", zOptional(zArr(zOptional(zObj(fl("type", zLit("reasoning_summary")), fl("text", zStr))))))),
		"function_call": zObj(fl("type", zLit("function_call")), fl("id", zStr), fl("call_id", zStr), fl("name", zStr), fl("arguments", zStr),
			fl("output", zOptional(zStr)), fl("status", zOptional(zStr))),
		"mcp_call": zObj(fl("type", zLit("mcp_call")), fl("id", zStr), fl("name", zStr), fl("arguments", zStr),
			fl("output", zOptional(zStr)), fl("status", zOptional(zStr))),
		"mcp_list_tools": zObj(fl("type", zLit("mcp_list_tools")), fl("id", zStr), fl("server_label", zStr),
			fl("tools", zOptional(zArr(zAny))), fl("status", zOptional(zStr))),
	}))),
	fl("output_text", zOptional(zNullable(zStr))),
)

// HuggingFaceResponsesResponseValidator is ChatResponseValidator for the
// Hugging Face Responses API (huggingfaceResponsesResponseSchema).
func HuggingFaceResponsesResponseValidator(providerName string) func(statusCode int, headers http.Header, body []byte) error {
	return responseValidator(providerName, func(body []byte) error {
		var value interface{}
		if err := json.Unmarshal(body, &value); err != nil {
			return err
		}
		if bad := zHuggingFaceResponse(value, true, nil); bad != nil {
			return bad
		}
		return nil
	})
}

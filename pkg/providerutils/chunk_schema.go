package providerutils

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// This file ports the zod chunk schemas the TS OpenAI-family providers apply
// to every parsed streaming frame (openaiChatChunkSchema,
// openaiCompletionChunkSchema, chunkBaseSchema of openai-compatible,
// groqChatChunkSchema, deepseekChatChunkSchema, moonshotAIChatChunkSchema,
// mistralChatChunkSchema, alibabaChatChunkSchema). zod objects are non-strict
// (unknown keys pass), `nullish` means absent or null, `optional` means absent
// only, and `nullable` means the key must be present but may be null.
//
// The combinators are shared with anthropic/chunk_schema.go and
// gemini/chunk_schema.go through pkg/internal/zodlite.

// ChunkSchemaError is a frame that failed a TS zod chunk schema. It is the
// shared zodlite issue; Error() renders "path: message".
type ChunkSchemaError = zodlite.Issue

// The combinators live in pkg/internal/zodlite (shared with the anthropic and
// gemini chunk schemas).
type (
	zNode = zodlite.Node
	zFld  = zodlite.Field
)

var (
	zType     = zodlite.JSType
	zBad      = zodlite.Invalid
	zStr      = zodlite.String
	zNum      = zodlite.Number
	zAny      = zodlite.Unknown
	zEnum     = zodlite.Enum
	zNullish  = zodlite.Nullish
	zNullable = zodlite.Nullable
	zOptional = zodlite.Optional
	zArr      = zodlite.Array
	zRec      = zodlite.RecordOf
	fl        = zodlite.F
	zObj      = zodlite.Object
	zUnionOf  = zodlite.Union
	zDiscType = zodlite.Disc
)

func zLit(want string) zNode { return zEnum(want) }

var (
	nStr  = zNullish(zStr)
	nNum  = zNullish(zNum)
	errID = zUnionOf(zStr, zNum)

	// openaiErrorDataSchema (openai, openai-compatible default, deepseek).
	zErrDefault = zObj(fl("error", zObj(fl("message", zStr), fl("type", nStr), fl("param", zAny), fl("code", zNullish(errID)))))
	// fireworks / baseten: error is a bare string or the OpenAI-shaped object.
	zErrStringOrObject = zObj(fl("error", zUnionOf(zStr, zObj(
		fl("message", zStr), fl("object", nStr), fl("type", nStr), fl("param", zAny), fl("code", zNullish(errID))))))
	// gmicloud: default plus error.details.
	zErrGmicloud = zObj(fl("error", zObj(fl("message", zStr), fl("type", nStr), fl("param", zAny), fl("code", zNullish(errID)), fl("details", nStr))))
	// cerebras: a flat object, every member a required string.
	zErrCerebras = zObj(fl("message", zStr), fl("type", zStr), fl("param", zStr), fl("code", zStr))
	// zai: error details at the top level or under `error`.
	zZaiDetails = zObj(fl("code", zNullish(zUnionOf(zNum, zStr))), fl("message", zStr))
	zErrZai     = zUnionOf(zZaiDetails, zObj(fl("error", zZaiDetails)))
	zErrGroq    = zObj(fl("error", zObj(fl("message", zStr), fl("type", zStr))))
	zErrMoon    = zObj(fl("error", zObj(fl("message", zStr), fl("type", nStr), fl("code", nStr))))

	// openaiCompatibleTokenUsageSchema.
	zCompatUsage = zNullish(zObj(
		fl("prompt_tokens", nNum), fl("completion_tokens", nNum), fl("total_tokens", nNum),
		fl("prompt_tokens_details", zNullish(zObj(fl("cached_tokens", nNum)))),
		fl("completion_tokens_details", zNullish(zObj(
			fl("reasoning_tokens", nNum), fl("accepted_prediction_tokens", nNum), fl("rejected_prediction_tokens", nNum)))),
	))

	zCompatContent = zNullish(zUnionOf(zStr, zArr(zObj(fl("type", zStr)))))

	zCompatChunk = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("choices", zArr(zObj(
			fl("delta", zNullish(zObj(
				fl("role", zNullish(zEnum("assistant", ""))),
				fl("content", zCompatContent),
				fl("reasoning_content", nStr), fl("reasoning", nStr),
				fl("tool_calls", zNullish(zArr(zObj(
					fl("index", nNum), fl("id", nStr),
					fl("function", zObj(fl("name", nStr), fl("arguments", nStr))),
					fl("extra_content", zNullish(zObj(fl("google", zNullish(zObj(fl("thought_signature", nStr))))))),
				)))),
			))),
			fl("finish_reason", nStr),
		))),
		fl("usage", zCompatUsage),
	)

	zOpenAIToolCalls = zNullish(zArr(zObj(
		fl("index", zNum), fl("id", nStr), fl("type", zNullish(zLit("function"))),
		fl("function", zObj(fl("name", nStr), fl("arguments", nStr))),
	)))

	zOpenAIChunk = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("choices", zArr(zObj(
			fl("delta", zNullish(zObj(
				fl("role", zNullish(zLit("assistant"))),
				fl("content", nStr),
				fl("tool_calls", zOpenAIToolCalls),
				fl("annotations", zNullish(zArr(zObj(
					fl("type", zLit("url_citation")),
					fl("url_citation", zObj(fl("start_index", zNum), fl("end_index", zNum), fl("url", zStr), fl("title", zStr))),
				)))),
			))),
			fl("logprobs", zNullish(zObj(fl("content", zNullish(zArr(zObj(
				fl("token", zStr), fl("logprob", zNum),
				fl("top_logprobs", zArr(zObj(fl("token", zStr), fl("logprob", zNum)))),
			))))))),
			fl("finish_reason", nStr),
			fl("index", zNum),
		))),
		fl("usage", zNullish(zObj(
			fl("prompt_tokens", nNum), fl("completion_tokens", nNum), fl("total_tokens", nNum),
			fl("prompt_tokens_details", zNullish(zObj(fl("cached_tokens", nNum), fl("cache_write_tokens", nNum)))),
			fl("completion_tokens_details", zNullish(zObj(
				fl("reasoning_tokens", nNum), fl("accepted_prediction_tokens", nNum), fl("rejected_prediction_tokens", nNum)))),
		))),
	)

	zCompletionChunk = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("choices", zArr(zObj(
			fl("text", zStr), fl("finish_reason", nStr), fl("index", zNum),
			fl("logprobs", zNullish(zObj(
				fl("tokens", zArr(zStr)), fl("token_logprobs", zArr(zNum)),
				fl("top_logprobs", zNullish(zArr(zRec(zNum)))),
			))),
		))),
		fl("usage", zNullish(zObj(fl("prompt_tokens", zNum), fl("completion_tokens", zNum), fl("total_tokens", zNum)))),
	)

	zGroqChunk = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("choices", zArr(zObj(
			fl("delta", zNullish(zObj(
				fl("content", nStr), fl("reasoning", nStr),
				fl("tool_calls", zNullish(zArr(zObj(
					fl("index", zNum), fl("id", nStr), fl("type", zOptional(zLit("function"))),
					fl("function", zObj(fl("name", nStr), fl("arguments", nStr))),
				)))),
			))),
			fl("finish_reason", zNullish(zStr)),
			fl("index", zNum),
		))),
		fl("x_groq", zNullish(zObj(fl("usage", zNullish(zObj(
			fl("prompt_tokens", nNum), fl("completion_tokens", nNum), fl("total_tokens", nNum),
			fl("prompt_tokens_details", zNullish(zObj(fl("cached_tokens", nNum)))),
			fl("completion_tokens_details", zNullish(zObj(fl("reasoning_tokens", nNum)))),
		)))))),
	)

	zBytes     = zNullable(zArr(zNum))
	zLogprobEl = zObj(
		fl("token", zStr), fl("logprob", zNum), fl("bytes", zBytes),
		fl("top_logprobs", zArr(zObj(fl("token", zStr), fl("logprob", zNum), fl("bytes", zBytes)))),
	)

	zDeepSeekUsage = zNullish(zObj(
		fl("prompt_tokens", nNum), fl("completion_tokens", nNum),
		fl("prompt_cache_hit_tokens", nNum), fl("prompt_cache_miss_tokens", nNum), fl("total_tokens", nNum),
		fl("prompt_tokens_details", zNullish(zObj(fl("cached_tokens", nNum)))),
		fl("completion_tokens_details", zNullish(zObj(fl("reasoning_tokens", nNum)))),
	))

	zDeepSeekChunk = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("object", zNullish(zLit("chat.completion.chunk"))), fl("system_fingerprint", nStr),
		fl("choices", zArr(zObj(
			fl("index", nNum),
			fl("delta", zNullish(zObj(
				fl("role", zNullish(zLit("assistant"))),
				fl("content", nStr), fl("reasoning_content", nStr),
				fl("tool_calls", zOpenAIToolCalls),
			))),
			fl("logprobs", zNullish(zObj(
				fl("content", zNullish(zArr(zLogprobEl))),
				fl("reasoning_content", zNullish(zArr(zLogprobEl))),
			))),
			fl("finish_reason", nStr),
		))),
		fl("usage", zDeepSeekUsage),
	)

	zMoonshotUsage = zNullish(zObj(
		fl("prompt_tokens", nNum), fl("completion_tokens", nNum), fl("cached_tokens", nNum), fl("total_tokens", nNum),
		fl("prompt_tokens_details", zNullish(zObj(fl("cached_tokens", nNum)))),
		fl("completion_tokens_details", zNullish(zObj(fl("reasoning_tokens", nNum)))),
	))

	zMoonshotChunk = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("object", zNullish(zLit("chat.completion.chunk"))),
		fl("choices", zArr(zObj(
			fl("index", nNum),
			fl("delta", zNullish(zObj(
				fl("role", zNullish(zLit("assistant"))),
				fl("content", nStr), fl("reasoning_content", nStr),
				fl("tool_calls", zNullish(zArr(zObj(
					fl("index", nNum), fl("id", nStr), fl("type", zNullish(zLit("function"))),
					fl("function", zObj(fl("name", nStr), fl("arguments", nStr))),
				)))),
			))),
			fl("logprobs", zNullish(zObj(fl("content", zNullish(zArr(zLogprobEl)))))),
			fl("finish_reason", nStr),
			fl("usage", zMoonshotUsage),
		))),
		fl("usage", zMoonshotUsage),
	)

	zMistralContent = zNullish(zUnionOf(zStr, zArr(zDiscType(map[string]zNode{
		"text":      zObj(fl("type", zLit("text")), fl("text", zStr)),
		"image_url": zObj(fl("type", zLit("image_url")), fl("image_url", zUnionOf(zStr, zObj(fl("url", zStr), fl("detail", zNullable(zStr)))))),
		"reference": zObj(fl("type", zLit("reference")), fl("reference_ids", zArr(errID))),
		"thinking": zObj(fl("type", zLit("thinking")), fl("thinking", zArr(
			zObj(fl("type", zLit("text")), fl("text", zStr))))),
	}))))

	zMistralDetails = zObj(fl("messages", zNullish(zArr(zAny))), fl("cached_tokens", nNum), fl("audio_tokens", nNum))

	zMistralChunk = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("choices", zArr(zObj(
			fl("delta", zObj(
				fl("role", zOptional(zLit("assistant"))),
				fl("content", zMistralContent),
				fl("tool_calls", zNullish(zArr(zObj(
					fl("index", nNum), fl("id", nStr),
					fl("function", zObj(fl("name", nStr), fl("arguments", nStr))),
				)))),
			)),
			fl("finish_reason", nStr),
			fl("index", zNum),
		))),
		fl("usage", zNullish(zObj(
			fl("prompt_tokens", zNum), fl("completion_tokens", zNum), fl("total_tokens", zNum),
			fl("prompt_audio_seconds", nNum), fl("request_count", nNum), fl("service_tier", nStr), fl("num_cached_tokens", nNum),
			fl("prompt_tokens_details", zNullish(zMistralDetails)),
			fl("prompt_token_details", zNullish(zMistralDetails)),
			fl("completion_tokens_details", zNullish(zObj(fl("reasoning_tokens", nNum)))),
		))),
	)

	zAlibabaChunk = zObj(
		fl("id", nStr), fl("created", nNum), fl("model", nStr),
		fl("choices", zArr(zObj(
			fl("delta", zObj(
				fl("role", zNullish(zLit("assistant"))),
				fl("content", nStr), fl("reasoning_content", nStr),
				fl("tool_calls", zNullish(zArr(zObj(
					fl("index", nNum), fl("id", nStr), fl("type", zNullish(zLit("function"))),
					fl("function", zNullish(zObj(fl("name", nStr), fl("arguments", nStr)))),
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

// ChatChunkKind selects which TS chunk schema a provider's stream is checked
// against. The zero value is the openai-compatible chunkBaseSchema.
type ChatChunkKind int

const (
	// ChunkKindOpenAICompatible is openai-compatible's chunkBaseSchema (also
	// used by the Go OpenAI chat model in openai-compatible mode).
	ChunkKindOpenAICompatible ChatChunkKind = iota
	// ChunkKindOpenAI is openaiChatChunkSchema (also azure).
	ChunkKindOpenAI
	// ChunkKindOpenAICompletion is openaiCompletionChunkSchema.
	ChunkKindOpenAICompletion
	ChunkKindGroq
	ChunkKindDeepSeek
	ChunkKindMoonshot
	// ChunkKindMistral and ChunkKindAlibaba have no error union member.
	ChunkKindMistral
	ChunkKindAlibaba
)

// ChunkErrorKind selects the error schema of an openai-compatible provider's
// chunk union (TS ProviderErrorStructure.errorSchema). The zero value is the
// default openaiCompatibleErrorDataSchema.
type ChunkErrorKind int

const (
	ChunkErrorDefault ChunkErrorKind = iota
	// ChunkErrorStringOrObject: fireworks, baseten (error is a string or object).
	ChunkErrorStringOrObject
	// ChunkErrorCerebras: a flat {message, type, param, code} object.
	ChunkErrorCerebras
	ChunkErrorGmicloud
	ChunkErrorZai
)

// ChatChunkShape selects the zod schema of a streamed chat-completion frame.
type ChatChunkShape struct {
	Kind ChatChunkKind
	// Error picks the error union member for ChunkKindOpenAICompatible.
	Error ChunkErrorKind
}

// ChunkErrorKindForProvider maps an openai-compatible provider name to the
// TS errorStructure its chunk union uses.
func ChunkErrorKindForProvider(name string) ChunkErrorKind {
	switch name {
	case "fireworks", "baseten":
		return ChunkErrorStringOrObject
	case "cerebras":
		return ChunkErrorCerebras
	case "gmicloud":
		return ChunkErrorGmicloud
	case "zai":
		return ChunkErrorZai
	}
	return ChunkErrorDefault
}

func (s ChatChunkShape) node() zNode {
	switch s.Kind {
	case ChunkKindOpenAI:
		return zUnionOf(zOpenAIChunk, zErrDefault)
	case ChunkKindOpenAICompletion:
		return zUnionOf(zCompletionChunk, zErrDefault)
	case ChunkKindGroq:
		return zUnionOf(zGroqChunk, zErrGroq)
	case ChunkKindDeepSeek:
		return zUnionOf(zDeepSeekChunk, zErrDefault)
	case ChunkKindMoonshot:
		return zUnionOf(zMoonshotChunk, zErrMoon)
	case ChunkKindMistral:
		return zMistralChunk
	case ChunkKindAlibaba:
		return zAlibabaChunk
	}
	errNode := zErrDefault
	switch s.Error {
	case ChunkErrorStringOrObject:
		errNode = zErrStringOrObject
	case ChunkErrorCerebras:
		errNode = zErrCerebras
	case ChunkErrorGmicloud:
		errNode = zErrGmicloud
	case ChunkErrorZai:
		errNode = zErrZai
	}
	return zUnionOf(zCompatChunk, errNode)
}

// ValidateChatChunk checks a streamed chat-completion frame against the TS zod
// chunk schema of shape. A nil result means TS's safeParse succeeds (which
// includes frames that match only the error member of the union; callers still
// route those to their error handling). The error is a *ChunkSchemaError, or
// the JSON syntax error when data is not valid JSON.
func ValidateChatChunk(data []byte, shape ChatChunkShape) error {
	var value interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	// The union's failure is reported at the root; for the single-schema kinds
	// the first failing member path is more useful, so re-run the object node.
	node := shape.node()
	if bad := node(value, true, nil); bad != nil {
		if shape.Kind == ChunkKindMistral || shape.Kind == ChunkKindAlibaba {
			return bad
		}
		if inner := shape.dataNode()(value, true, nil); inner != nil {
			return inner
		}
		return bad
	}
	return nil
}

// dataNode is the non-error member of the chunk union.
func (s ChatChunkShape) dataNode() zNode {
	switch s.Kind {
	case ChunkKindOpenAI:
		return zOpenAIChunk
	case ChunkKindOpenAICompletion:
		return zCompletionChunk
	case ChunkKindGroq:
		return zGroqChunk
	case ChunkKindDeepSeek:
		return zDeepSeekChunk
	case ChunkKindMoonshot:
		return zMoonshotChunk
	case ChunkKindMistral:
		return zMistralChunk
	case ChunkKindAlibaba:
		return zAlibabaChunk
	}
	return zCompatChunk
}

// ChunkParseFailure builds the typed error for a stream frame the TS provider
// would turn into an `error` part: a *JSONParseError for text that is not JSON
// (TS AI_JSONParseError), otherwise a *ValidationError (TS
// TypeValidationError) for a schema or typed-decode failure, so errors.As
// works through core. The returned message is the error's text.
func ChunkParseFailure(data string, cause error) (string, error) {
	var syntax error
	if jerr := json.Unmarshal([]byte(data), new(interface{})); jerr != nil {
		syntax = jerr
	}
	if syntax != nil {
		err := providererrors.NewJSONParseError(data, syntax)
		return err.Error(), err
	}
	compact := data
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(data)); err == nil {
		compact = b.String()
	}
	issue := map[string]interface{}{"code": "custom", "path": []interface{}{}, "message": "Invalid input"}
	if se, ok := cause.(*ChunkSchemaError); ok {
		path := se.Path
		if path == nil {
			path = []interface{}{}
		}
		issue = map[string]interface{}{"code": se.Code, "path": path, "message": se.Message}
	} else if cause != nil {
		issue["message"] = cause.Error()
	}
	var issues bytes.Buffer
	enc := json.NewEncoder(&issues)
	enc.SetEscapeHTML(false)
	_ = enc.Encode([]interface{}{issue})
	issueText := strings.TrimSpace(issues.String())
	err := providererrors.NewValidationErrorWithContext(compact, issueText, nil, &providererrors.ValidationContext{})
	return err.Error(), err
}

// CheckChatChunk validates data against shape and, on failure, returns the TS
// error-part message and its typed error (see ChunkParseFailure); a frame that
// passes returns "", nil.
func CheckChatChunk(data string, shape ChatChunkShape) (string, error) {
	err := ValidateChatChunk([]byte(data), shape)
	if err == nil {
		return "", nil
	}
	if _, ok := err.(*ChunkSchemaError); !ok {
		err = nil // not JSON: ChunkParseFailure reports the parse failure
	}
	return ChunkParseFailure(data, err)
}

// CheckTypedFrame validates the last union member of a TS Responses-style
// chunk schema, `{type: z.string()}.loose()`: a frame without a string `type`
// fails every member. It returns the error-part message and typed error.
func CheckTypedFrame(data string) (string, error) {
	var value interface{}
	if err := json.Unmarshal([]byte(data), &value); err != nil {
		return ChunkParseFailure(data, nil)
	}
	if bad := zObj(fl("type", zStr))(value, true, nil); bad != nil {
		return ChunkParseFailure(data, bad)
	}
	return "", nil
}

// UnmarshalChunk decodes a schema-validated stream frame into v. zod's
// z.number() accepts fractional and exponent forms (1.5, 2.0, 1e9) that Go's
// integer struct fields reject; when the strict decode fails on such a number,
// the frame is retried with every non-integral number truncated and integral
// ones (2.0, 1e9) written as integers, so a frame TS accepts is never rejected
// for its number spelling.
func UnmarshalChunk(data []byte, v interface{}) error {
	err := json.Unmarshal(data, v)
	var typeErr *json.UnmarshalTypeError
	if err == nil || !errors.As(err, &typeErr) || !strings.HasPrefix(typeErr.Value, "number") {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var tree interface{}
	if dec.Decode(&tree) != nil {
		return err
	}
	normalized, merr := json.Marshal(normalizeNumbers(tree))
	if merr != nil {
		return err
	}
	return json.Unmarshal(normalized, v)
}

func normalizeNumbers(v interface{}) interface{} {
	switch x := v.(type) {
	case json.Number:
		if _, err := x.Int64(); err == nil {
			return x
		}
		f, err := x.Float64()
		if err != nil {
			return x
		}
		if f >= -9.2e18 && f <= 9.2e18 {
			return json.Number(strconv.FormatInt(int64(f), 10))
		}
		return x
	case []interface{}:
		for i := range x {
			x[i] = normalizeNumbers(x[i])
		}
		return x
	case map[string]interface{}:
		for k := range x {
			x[k] = normalizeNumbers(x[k])
		}
		return x
	}
	return v
}

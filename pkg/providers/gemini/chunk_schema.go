package gemini

import (
	"bytes"
	"encoding/json"
	"strings"
)

// This file ports the zod `chunkSchema` / `responseSchema`
// (google-language-model.ts) that TS applies to every parsed Gemini/Vertex
// response frame. zod objects are non-strict (unknown keys pass), `nullish`
// means absent or null. A frame that does not match becomes an `error` part
// (TypeValidationError in TS) and the stream keeps reading.

type zIssue struct {
	path    []interface{}
	code    string
	message string
}

// zNode validates one value; present is false when the object key is absent.
type zNode func(v interface{}, present bool, path []interface{}) *zIssue

func zJSType(v interface{}, present bool) string {
	switch v.(type) {
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
	}
	return "unknown"
}

func zInvalid(expected string, v interface{}, present bool, path []interface{}) *zIssue {
	return &zIssue{path: path, code: "invalid_type", message: "Invalid input: expected " + expected + ", received " + zJSType(v, present)}
}

func zStr(v interface{}, present bool, path []interface{}) *zIssue {
	if _, ok := v.(string); !ok {
		return zInvalid("string", v, present, path)
	}
	return nil
}

func zNum(v interface{}, present bool, path []interface{}) *zIssue {
	if _, ok := v.(float64); !ok {
		return zInvalid("number", v, present, path)
	}
	return nil
}

func zBool(v interface{}, present bool, path []interface{}) *zIssue {
	if _, ok := v.(bool); !ok {
		return zInvalid("boolean", v, present, path)
	}
	return nil
}

func zAny(interface{}, bool, []interface{}) *zIssue { return nil }

func zNullish(n zNode) zNode {
	return func(v interface{}, present bool, path []interface{}) *zIssue {
		if !present || v == nil {
			return nil
		}
		return n(v, present, path)
	}
}

func zArr(n zNode) zNode {
	return func(v interface{}, present bool, path []interface{}) *zIssue {
		a, ok := v.([]interface{})
		if !ok {
			return zInvalid("array", v, present, path)
		}
		for i, e := range a {
			if bad := n(e, true, append(append([]interface{}{}, path...), i)); bad != nil {
				return bad
			}
		}
		return nil
	}
}

type zFld struct {
	key  string
	node zNode
}

func zObj(fields ...zFld) zNode {
	return func(v interface{}, present bool, path []interface{}) *zIssue {
		m, ok := v.(map[string]interface{})
		if !ok {
			return zInvalid("object", v, present, path)
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

func zUnionOf(nodes ...zNode) zNode {
	return func(v interface{}, present bool, path []interface{}) *zIssue {
		for _, n := range nodes {
			if n(v, present, path) == nil {
				return nil
			}
		}
		return &zIssue{path: path, code: "invalid_union", message: "Invalid input"}
	}
}

func fl(key string, n zNode) zFld { return zFld{key, n} }

var (
	zSig         = fl("thoughtSignature", zNullish(zStr))
	zTokenDetail = zNullish(zArr(zObj(fl("modality", zStr), fl("tokenCount", zNum))))

	zPart = zUnionOf(
		// note: order matters in TS since text can be fully empty
		zObj(fl("functionCall", zObj(
			fl("id", zNullish(zStr)), fl("name", zNullish(zStr)), fl("args", zAny),
			fl("partialArgs", zNullish(zArr(zObj(
				fl("jsonPath", zStr), fl("stringValue", zNullish(zStr)), fl("numberValue", zNullish(zNum)),
				fl("boolValue", zNullish(zBool)), fl("nullValue", zAny), fl("willContinue", zNullish(zBool)),
			)))),
			fl("willContinue", zNullish(zBool)),
		)), zSig),
		zObj(fl("inlineData", zObj(fl("mimeType", zStr), fl("data", zStr))), fl("thought", zNullish(zBool)), zSig),
		zObj(fl("toolCall", zObj(fl("toolType", zStr), fl("args", zAny), fl("id", zStr))), zSig),
		zObj(fl("toolResponse", zObj(fl("toolType", zStr), fl("response", zAny), fl("id", zStr))), zSig),
		zObj(
			fl("executableCode", zNullish(zObj(fl("language", zStr), fl("code", zStr)))),
			fl("codeExecutionResult", zNullish(zObj(fl("outcome", zStr), fl("output", zNullish(zStr))))),
			fl("text", zNullish(zStr)), fl("thought", zNullish(zBool)), zSig,
		),
	)

	zSafety = zObj(
		fl("category", zNullish(zStr)), fl("probability", zNullish(zStr)), fl("probabilityScore", zNullish(zNum)),
		fl("severity", zNullish(zStr)), fl("severityScore", zNullish(zNum)), fl("blocked", zNullish(zBool)),
	)

	zStrArr = zNullish(zArr(zStr))
	zNumArr = zNullish(zArr(zNum))

	zGrounding = zObj(
		fl("webSearchQueries", zStrArr), fl("imageSearchQueries", zStrArr), fl("retrievalQueries", zStrArr),
		fl("searchEntryPoint", zNullish(zObj(fl("renderedContent", zStr)))),
		fl("groundingChunks", zNullish(zArr(zObj(
			fl("web", zNullish(zObj(fl("uri", zStr), fl("title", zNullish(zStr))))),
			fl("image", zNullish(zObj(fl("sourceUri", zStr), fl("imageUri", zStr), fl("title", zNullish(zStr)), fl("domain", zNullish(zStr))))),
			fl("retrievedContext", zNullish(zObj(
				fl("uri", zNullish(zStr)), fl("title", zNullish(zStr)), fl("text", zNullish(zStr)), fl("fileSearchStore", zNullish(zStr)),
				fl("customMetadata", zNullish(zArr(zObj(
					fl("key", zStr), fl("stringValue", zNullish(zStr)), fl("numericValue", zNullish(zNum)),
					fl("stringListValue", zNullish(zObj(fl("values", zStrArr)))),
				)))),
			))),
			fl("maps", zNullish(zObj(fl("uri", zNullish(zStr)), fl("title", zNullish(zStr)), fl("text", zNullish(zStr)), fl("placeId", zNullish(zStr))))),
		)))),
		fl("groundingSupports", zNullish(zArr(zObj(
			fl("segment", zNullish(zObj(fl("startIndex", zNullish(zNum)), fl("endIndex", zNullish(zNum)), fl("text", zNullish(zStr))))),
			fl("segment_text", zNullish(zStr)), fl("groundingChunkIndices", zNumArr), fl("supportChunkIndices", zNumArr),
			fl("confidenceScores", zNumArr), fl("confidenceScore", zNumArr),
		)))),
		// union([{webDynamicRetrievalScore: number}, {}]): any object passes.
		fl("retrievalMetadata", zNullish(zObj())),
	)

	zUsage = zObj(
		fl("cachedContentTokenCount", zNullish(zNum)), fl("thoughtsTokenCount", zNullish(zNum)),
		fl("promptTokenCount", zNullish(zNum)), fl("candidatesTokenCount", zNullish(zNum)),
		fl("toolUsePromptTokenCount", zNullish(zNum)), fl("totalTokenCount", zNullish(zNum)),
		fl("trafficType", zNullish(zStr)), fl("serviceTier", zNullish(zStr)),
		fl("promptTokensDetails", zTokenDetail), fl("cacheTokensDetails", zTokenDetail),
		fl("candidatesTokensDetails", zTokenDetail), fl("toolUsePromptTokensDetails", zTokenDetail),
	)

	zChunk = zObj(
		fl("responseId", zNullish(zStr)),
		fl("candidates", zNullish(zArr(zObj(
			fl("content", zNullish(zObj(fl("parts", zNullish(zArr(zPart)))))),
			fl("finishReason", zNullish(zStr)), fl("finishMessage", zNullish(zStr)), fl("continuationToken", zNullish(zStr)),
			fl("safetyRatings", zNullish(zArr(zSafety))),
			fl("groundingMetadata", zNullish(zGrounding)),
			fl("urlContextMetadata", zNullish(zObj(fl("urlMetadata", zNullish(zArr(zObj(fl("retrievedUrl", zStr), fl("urlRetrievalStatus", zStr)))))))),
		)))),
		fl("usageMetadata", zNullish(zUsage)),
		fl("promptFeedback", zNullish(zObj(fl("blockReason", zNullish(zStr)), fl("safetyRatings", zNullish(zArr(zSafety)))))),
	)
)

// chunkValidationMessage validates a JSON frame against the TS chunkSchema.
// It returns "" when the frame matches (or is not JSON, which is reported
// separately), otherwise the TS TypeValidationError message. The zod issue
// text is a best-effort rendering (path plus the zod v4 message of the first
// failing field).
func chunkValidationMessage(data string) string {
	var value interface{}
	if err := json.Unmarshal([]byte(data), &value); err != nil {
		return ""
	}
	bad := zChunk(value, true, nil)
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

// validationIssueText returns the issue list that follows "Error message: ".
func validationIssueText(message string) string {
	if _, after, ok := strings.Cut(message, "Error message: "); ok {
		return after
	}
	return message
}

func compactJSONString(data string) string {
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(data)); err != nil {
		return data
	}
	return b.String()
}

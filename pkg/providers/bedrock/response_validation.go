package bedrock

import (
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// This file ports the zod schemas AmazonBedrockResponseSchema and
// AmazonBedrockStreamSchema (amazon-bedrock-chat-language-model.ts). The
// schemas are non-strict (unknown keys pass) and "nullish" means absent or
// null. A mismatch is reported with the path of the offending field.

type schemaPath string

func (p schemaPath) key(k string) schemaPath {
	if p == "" {
		return schemaPath(k)
	}
	return schemaPath(string(p) + "." + k)
}

func (p schemaPath) index(i int) schemaPath { return schemaPath(fmt.Sprintf("%s[%d]", p, i)) }

type schemaIssue struct{ path, problem string }

func (i *schemaIssue) validation() *providererrors.ValidationError {
	return &providererrors.ValidationError{
		Field:   i.path,
		Message: fmt.Sprintf(`Type validation failed: invalid value at "%s": %s`, i.path, i.problem),
	}
}

func issue(p schemaPath, problem string) *schemaIssue {
	return &schemaIssue{path: string(p), problem: problem}
}

func isNullish(v interface{}, present bool) bool { return !present || v == nil }

func asObject(v interface{}, p schemaPath) (map[string]interface{}, *schemaIssue) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, issue(p, "expected object")
	}
	return m, nil
}

// field kinds
func reqString(m map[string]interface{}, k string, p schemaPath) *schemaIssue {
	if _, ok := m[k].(string); !ok {
		return issue(p.key(k), "expected string")
	}
	return nil
}

func nullishString(m map[string]interface{}, k string, p schemaPath) *schemaIssue {
	v, present := m[k]
	if isNullish(v, present) {
		return nil
	}
	if _, ok := v.(string); !ok {
		return issue(p.key(k), "expected string")
	}
	return nil
}

func reqNumber(m map[string]interface{}, k string, p schemaPath) *schemaIssue {
	if _, ok := m[k].(float64); !ok {
		return issue(p.key(k), "expected number")
	}
	return nil
}

func nullishNumber(m map[string]interface{}, k string, p schemaPath) *schemaIssue {
	v, present := m[k]
	if isNullish(v, present) {
		return nil
	}
	if _, ok := v.(float64); !ok {
		return issue(p.key(k), "expected number")
	}
	return nil
}

// nullishObject runs check on m[k] when it is present and not null.
func nullishObject(m map[string]interface{}, k string, p schemaPath, check func(map[string]interface{}, schemaPath) *schemaIssue) *schemaIssue {
	v, present := m[k]
	if isNullish(v, present) {
		return nil
	}
	o, bad := asObject(v, p.key(k))
	if bad != nil {
		return bad
	}
	return check(o, p.key(k))
}

func firstIssue(issues ...*schemaIssue) *schemaIssue {
	for _, i := range issues {
		if i != nil {
			return i
		}
	}
	return nil
}

func checkStopReason(m map[string]interface{}, p schemaPath) *schemaIssue {
	return reqString(m, "stopReason", p)
}

func checkAdditionalModelResponseFields(o map[string]interface{}, p schemaPath) *schemaIssue {
	return nullishObject(o, "delta", p, func(d map[string]interface{}, dp schemaPath) *schemaIssue {
		return nullishString(d, "stop_sequence", dp)
	})
}

func checkToolUse(o map[string]interface{}, p schemaPath) *schemaIssue {
	return firstIssue(reqString(o, "toolUseId", p), reqString(o, "name", p))
}

func checkLatencyConfig(o map[string]interface{}, p schemaPath) *schemaIssue {
	return reqString(o, "latency", p)
}

func checkServiceTier(o map[string]interface{}, p schemaPath) *schemaIssue {
	return reqString(o, "type", p)
}

// checkUsage validates the usage object; totalTokens is required in the
// response and optional in the stream metadata.
func checkUsage(u map[string]interface{}, p schemaPath, totalRequired bool) *schemaIssue {
	if bad := firstIssue(reqNumber(u, "inputTokens", p), reqNumber(u, "outputTokens", p)); bad != nil {
		return bad
	}
	if totalRequired {
		if bad := reqNumber(u, "totalTokens", p); bad != nil {
			return bad
		}
	} else if bad := nullishNumberOptional(u, "totalTokens", p); bad != nil {
		return bad
	}
	if bad := firstIssue(nullishNumber(u, "cacheReadInputTokens", p), nullishNumber(u, "cacheWriteInputTokens", p)); bad != nil {
		return bad
	}
	cd, present := u["cacheDetails"]
	if isNullish(cd, present) {
		return nil
	}
	arr, ok := cd.([]interface{})
	if !ok {
		return issue(p.key("cacheDetails"), "expected array")
	}
	for i, e := range arr {
		ep := p.key("cacheDetails").index(i)
		o, bad := asObject(e, ep)
		if bad != nil {
			return bad
		}
		if bad := firstIssue(reqNumber(o, "inputTokens", ep), reqString(o, "ttl", ep)); bad != nil {
			return bad
		}
	}
	return nil
}

// nullishNumberOptional is z.number().optional(): absent passes, null does not.
func nullishNumberOptional(m map[string]interface{}, k string, p schemaPath) *schemaIssue {
	v, present := m[k]
	if !present {
		return nil
	}
	if _, ok := v.(float64); !ok {
		return issue(p.key(k), "expected number")
	}
	return nil
}

func checkReasoningContentBlock(rc map[string]interface{}, p schemaPath) *schemaIssue {
	// union of reasoningText | redactedReasoning | redactedContent
	if v, ok := rc["reasoningText"]; ok {
		if o, ok := v.(map[string]interface{}); ok {
			if firstIssue(nullishString(o, "signature", p), reqString(o, "text", p)) == nil {
				return nil
			}
		}
	}
	if v, ok := rc["redactedReasoning"]; ok {
		if o, ok := v.(map[string]interface{}); ok && reqString(o, "data", p) == nil {
			return nil
		}
	}
	if _, ok := rc["redactedContent"].(string); ok {
		return nil
	}
	return issue(p, "invalid input")
}

// validateConverseResponse validates a decoded Converse response body.
func validateConverseResponse(body interface{}) *providererrors.ValidationError {
	root, bad := asObject(body, "")
	if bad == nil {
		bad = checkConverseResponse(root)
	}
	if bad != nil {
		return bad.validation()
	}
	return nil
}

func checkConverseResponse(root map[string]interface{}) *schemaIssue {
	var p schemaPath
	if bad := nullishObject(root, "metrics", p, func(o map[string]interface{}, mp schemaPath) *schemaIssue {
		return reqNumber(o, "latencyMs", mp)
	}); bad != nil {
		return bad
	}
	out, bad := asObject(root["output"], p.key("output"))
	if bad != nil {
		return bad
	}
	msg, bad := asObject(out["message"], p.key("output").key("message"))
	if bad != nil {
		return bad
	}
	mp := p.key("output").key("message")
	content, ok := msg["content"].([]interface{})
	if !ok {
		return issue(mp.key("content"), "expected array")
	}
	for i, e := range content {
		cp := mp.key("content").index(i)
		block, bad := asObject(e, cp)
		if bad != nil {
			return bad
		}
		if bad := checkContentBlock(block, cp); bad != nil {
			return bad
		}
	}
	if bad := reqString(msg, "role", mp); bad != nil {
		return bad
	}
	if bad := checkStopReason(root, p); bad != nil {
		return bad
	}
	if bad := firstIssue(
		nullishObject(root, "additionalModelResponseFields", p, checkAdditionalModelResponseFields),
		nullishObject(root, "performanceConfig", p, checkLatencyConfig),
		nullishObject(root, "serviceTier", p, checkServiceTier),
	); bad != nil {
		return bad
	}
	usage, bad := asObject(root["usage"], p.key("usage"))
	if bad != nil {
		return bad
	}
	return checkUsage(usage, p.key("usage"), true)
}

func checkContentBlock(block map[string]interface{}, p schemaPath) *schemaIssue {
	if bad := nullishString(block, "text", p); bad != nil {
		return bad
	}
	if bad := nullishObject(block, "citationsContent", p, func(cc map[string]interface{}, cp schemaPath) *schemaIssue {
		v, present := cc["content"]
		if isNullish(v, present) {
			return nil
		}
		arr, ok := v.([]interface{})
		if !ok {
			return issue(cp.key("content"), "expected array")
		}
		for i, e := range arr {
			o, bad := asObject(e, cp.key("content").index(i))
			if bad != nil {
				return bad
			}
			if bad := nullishString(o, "text", cp.key("content").index(i)); bad != nil {
				return bad
			}
		}
		return nil
	}); bad != nil {
		return bad
	}
	if bad := nullishObject(block, "toolUse", p, checkToolUse); bad != nil {
		return bad
	}
	return nullishObject(block, "reasoningContent", p, checkReasoningContentBlock)
}

// validateConverseStreamEvent validates one stream event wrapped under its
// payload type, as the TS event-stream handler produces it.
func validateConverseStreamEvent(payloadType string, payload interface{}) *providererrors.ValidationError {
	if bad := checkStreamEvent(payloadType, payload); bad != nil {
		return bad.validation()
	}
	return nil
}

func checkStreamEvent(payloadType string, payload interface{}) *schemaIssue {
	p := schemaPath(payloadType)
	if payload == nil {
		return nil // every member is nullish
	}
	switch payloadType {
	case "internalServerException", "modelStreamErrorException", "serviceUnavailableException", "throttlingException", "validationException":
		if _, bad := asObject(payload, p); bad != nil {
			return bad
		}
	case "contentBlockDelta":
		o, bad := asObject(payload, p)
		if bad != nil {
			return bad
		}
		if bad := reqNumber(o, "contentBlockIndex", p); bad != nil {
			return bad
		}
		d, present := o["delta"]
		if isNullish(d, present) {
			return nil
		}
		do, bad := asObject(d, p.key("delta"))
		if bad != nil {
			return bad
		}
		if !streamDeltaMatches(do) {
			return issue(p.key("delta"), "invalid input")
		}
	case "contentBlockStart":
		o, bad := asObject(payload, p)
		if bad != nil {
			return bad
		}
		if bad := reqNumber(o, "contentBlockIndex", p); bad != nil {
			return bad
		}
		return nullishObject(o, "start", p, func(s map[string]interface{}, sp schemaPath) *schemaIssue {
			return nullishObject(s, "toolUse", sp, checkToolUse)
		})
	case "contentBlockStop":
		o, bad := asObject(payload, p)
		if bad != nil {
			return bad
		}
		return reqNumber(o, "contentBlockIndex", p)
	case "messageStop":
		o, bad := asObject(payload, p)
		if bad != nil {
			return bad
		}
		return firstIssue(
			nullishObject(o, "additionalModelResponseFields", p, checkAdditionalModelResponseFields),
			checkStopReason(o, p),
		)
	case "metadata":
		o, bad := asObject(payload, p)
		if bad != nil {
			return bad
		}
		return firstIssue(
			nullishObject(o, "performanceConfig", p, checkLatencyConfig),
			nullishObject(o, "serviceTier", p, checkServiceTier),
			nullishObject(o, "usage", p, func(u map[string]interface{}, up schemaPath) *schemaIssue {
				return checkUsage(u, up, false)
			}),
		)
	}
	return nil
}

func streamDeltaMatches(d map[string]interface{}) bool {
	if _, ok := d["text"].(string); ok {
		return true
	}
	if _, ok := d["citation"].(map[string]interface{}); ok {
		return true
	}
	if tu, ok := d["toolUse"].(map[string]interface{}); ok {
		if _, ok := tu["input"].(string); ok {
			return true
		}
	}
	if rc, ok := d["reasoningContent"].(map[string]interface{}); ok {
		for _, k := range []string{"text", "signature", "data", "redactedContent"} {
			if _, ok := rc[k].(string); ok {
				return true
			}
		}
	}
	return false
}

// invalidConverseResponseError is the APICallError TS throws when the
// response body does not match the schema.
func invalidConverseResponseError(cause error) error {
	return providererrors.NewProviderError("amazon-bedrock", 0, "", "Invalid JSON response", cause)
}

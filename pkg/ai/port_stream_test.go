package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS 6815153d64: a provider-executed preliminary tool-result is forwarded with
// its preliminary flag and is not recorded as a final result in content.
func TestPortStreamPreliminaryProviderToolResult(t *testing.T) {
	t.Parallel()
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "image_generation", RawArguments: "{}", ProviderExecuted: true}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "image_generation", Result: map[string]interface{}{"result": "partial-image"}, Preliminary: true}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "image_generation", Result: map[string]interface{}{"result": "final-image"}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}}
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var seen []bool
	fs := res.FullStream()
	for {
		c, err := fs.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeToolResult && c.ToolResult != nil {
			seen = append(seen, c.ToolResult.Preliminary)
		}
	}
	if len(seen) != 2 || !seen[0] || seen[1] {
		t.Fatalf("stream tool-result preliminary flags = %v, want [true false]", seen)
	}
	n := 0
	for _, p := range res.Content() {
		if tr, ok := p.(types.ToolResultContent); ok {
			n++
			if tr.Preliminary || tr.Result.(map[string]interface{})["result"] != "final-image" {
				t.Fatalf("content tool-result = %+v, want final-image", tr)
			}
		}
	}
	if n != 1 {
		t.Fatalf("content tool-results = %d, want 1", n)
	}
}

func portstreamRepeat(n int, elem string) string {
	return "[" + strings.TrimSuffix(strings.Repeat(elem+",", n), ",") + "]"
}

func portstreamValidationMessage(t *testing.T, messages string) string {
	t.Helper()
	res := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: json.RawMessage(messages)})
	if res.Success {
		t.Fatalf("validation succeeded for %.80s", messages)
	}
	if !providererrors.IsValidationError(res.Error) {
		t.Fatalf("error %T is not a validation error", res.Error)
	}
	msg := res.Error.Error()
	if len(msg) >= 40_000 {
		t.Fatalf("error message is %d bytes, want < 40000", len(msg))
	}
	return msg
}

// TS a3c6e3fef0 "should avoid union fan-out for malformed metadata".
func TestPortStreamValidateUIMessagesNoUnionFanOutForMalformedMetadata(t *testing.T) {
	t.Parallel()
	var meta strings.Builder
	meta.WriteString("{")
	for i := 0; i < 100; i++ {
		if i > 0 {
			meta.WriteString(",")
		}
		fmt.Fprintf(&meta, `"key%d":1`, i)
	}
	meta.WriteString("}")
	for _, part := range []string{
		`"x":1`,
		`"type":"unknown"`,
		`"type":"text"`,
		`"type":"dynamic-tool","toolName":"foo","toolCallId":"1","state":"input-available","input":{}`,
		`"type":"tool-foo","toolCallId":"1","state":"input-available","input":{}`,
	} {
		portstreamValidationMessage(t, `[{"id":"1","role":"user","parts":[{`+part+`,"callProviderMetadata":`+meta.String()+`}]}]`)
	}
}

// TS a3c6e3fef0 / 8c5d5345c0 "should stop accumulating structural errors across
// invalid parts, messages and steering parts": 7.5 KB of malformed input stays
// a single bounded issue at the first invalid element.
func TestPortStreamValidateUIMessagesStopsAtFirstInvalidElement(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, messages, path string
	}{
		{"parts", `[{"id":"1","role":"user","parts":` + portstreamRepeat(2500, `{}`) + `}]`, "[0].parts[0].type"},
		{"messages", portstreamRepeat(100, `{"id":"1","role":"user","parts":[{}]}`), "[0].parts[0].type"},
		{"steering parts", `[{"id":"1","role":"assistant","parts":[{"type":"steering-message","id":"2","parts":` + portstreamRepeat(2500, `{}`) + `}]}]`, "[0].parts[0]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := portstreamValidationMessage(t, tc.messages)
			if !strings.HasPrefix(msg, "Type validation failed") && !strings.Contains(msg, tc.path+":") {
				t.Fatalf("message %.200q does not report path %s", msg, tc.path)
			}
			if strings.Count(msg, "Invalid") > 3 {
				t.Fatalf("message accumulates several issues: %.300q", msg)
			}
		})
	}
}

// TS a3c6e3fef0 "should preserve the index of the first invalid part" and
// "should not validate parts after the first invalid part".
func TestPortStreamValidateUIMessagesFirstInvalidPartIndex(t *testing.T) {
	t.Parallel()
	msg := portstreamValidationMessage(t, `[{"id":"1","role":"user","parts":[{"type":"text","text":"hello"},{},{"type":"nope"}]}]`)
	if !strings.Contains(msg, "[0].parts[1].type") {
		t.Fatalf("message %.200q does not report the first invalid part [0].parts[1].type", msg)
	}
	if strings.Contains(msg, "parts[2]") {
		t.Fatalf("message %.200q reports a later part", msg)
	}
}

// TS a3c6e3fef0 "should continue to accept large valid part arrays".
func TestPortStreamValidateUIMessagesAcceptsLargeValidPartArrays(t *testing.T) {
	t.Parallel()
	msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
		Messages: json.RawMessage(`[{"id":"1","role":"user","parts":` + portstreamRepeat(2500, `{"type":"text","text":"hello"}`) + `}]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || len(msgs[0].Parts) != 2500 {
		t.Fatalf("got %d messages / %d parts, want 1 / 2500", len(msgs), len(msgs[0].Parts))
	}
}

func portstreamNestedArrays() string {
	elem := strings.Repeat("[", 100) + "[]" + strings.Repeat("]", 100)
	return portstreamRepeat(20, elem)
}

// TS 718824cd59 "valid JSON values in metadata": nested JSON values in provider,
// tool and citation metadata are accepted and keep their shape.
func TestPortStreamValidateUIMessagesAcceptsNestedJSONMetadata(t *testing.T) {
	t.Parallel()
	nested := portstreamNestedArrays()
	for name, part := range map[string]string{
		"provider metadata": `{"type":"text","text":"Hello","providerMetadata":{"provider":{"key":` + nested + `}}}`,
		"tool metadata and call/result provider metadata": `{"type":"tool-foo","toolCallId":"1","state":"output-available","input":{},"output":"result",` +
			`"callProviderMetadata":{"provider":{"key":` + nested + `}},"resultProviderMetadata":{"provider":{"key":` + nested + `}},"toolMetadata":{"key":` + nested + `}}`,
		"citation provider metadata": `{"type":"text","text":"Hello","citations":[{"source":{"type":"source","sourceType":"url","id":"source-1","url":"https://example.com","providerMetadata":{"provider":{"key":` + nested + `}}}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			msgs, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
				Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[` + part + `]}]`),
			})
			if err != nil {
				t.Fatal(err)
			}
			out, err := json.Marshal(msgs[0].Parts[0])
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(out), nested) {
				t.Fatalf("validated part lost the nested value: %.200s", out)
			}
		})
	}
}

// TS 718824cd59 "should reject provider metadata that is an array / a provider
// without an object / an undefined provider" and "should reject tool metadata
// that is not an object".
func TestPortStreamValidateUIMessagesRejectsMalformedMetadataShapes(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, part, path string }{
		{"array provider metadata", `{"type":"text","text":"Hello","providerMetadata":[]}`, "[0].parts[0]"},
		{"provider without an object", `{"type":"text","text":"Hello","providerMetadata":{"provider":"value"}}`, "[0].parts[0]"},
		{"null provider", `{"type":"text","text":"Hello","providerMetadata":{"provider":null}}`, "[0].parts[0]"},
		{"tool metadata array", `{"type":"dynamic-tool","toolName":"foo","toolCallId":"1","state":"input-available","input":{},"toolMetadata":["value"]}`, "[0].parts[0]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := portstreamValidationMessage(t, `[{"id":"1","role":"assistant","parts":[`+tc.part+`]}]`)
			if !strings.Contains(msg, tc.path) {
				t.Fatalf("message %.200q does not report %s", msg, tc.path)
			}
		})
	}
}

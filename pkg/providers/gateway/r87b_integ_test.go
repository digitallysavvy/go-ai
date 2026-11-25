package gateway

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func r87bintegGatewayParts(t *testing.T, events ...string) ([]string, error) {
	t.Helper()
	var lines []string
	for _, e := range events {
		lines = append(lines, "data: "+e, "")
	}
	lines = append(lines, "data: [DONE]", "")
	stream := &gatewayTextStream{
		parser: streamingParser(strings.Join(lines, "\n")),
		body:   io.NopCloser(strings.NewReader("")),
	}
	var got []string
	for {
		c, err := stream.Next()
		if err != nil {
			if err == io.EOF {
				return got, nil
			}
			return got, err
		}
		switch c.Type {
		case provider.ChunkTypeText:
			got = append(got, "text "+c.ID+" "+c.Text)
		case provider.ChunkTypeToolInputStart:
			got = append(got, "tool-input-start "+c.ID+" "+c.ToolCall.ToolName)
		case provider.ChunkTypeToolInputDelta:
			got = append(got, "tool-input-delta "+c.ID+" "+c.Text)
		case provider.ChunkTypeFinish:
			got = append(got, "finish "+string(c.FinishReason))
		default:
			got = append(got, string(c.Type))
		}
	}
}

// provb CR 1 (class sweep of V061 35): TS 3fab50aa9b gateway-language-model.ts
// parses stream parts with createEventSourceResponseHandler(z.any()) and
// enqueues them verbatim. Recorded with bun on the TS doStream: every part
// below is delivered as sent (`{"type":"text-delta","id":7,"delta":"a"}`, the
// bare `5` and `[1]` values, a finish with `finishReason: 7`), and only
// invalid JSON errors the stream. Go reads a non-string id / delta / tool
// name as JS String(value); a non-object part has no type and is skipped.
func TestR87bintegGatewayStreamLenientPartDecode(t *testing.T) {
	got, err := r87bintegGatewayParts(t,
		`{"type":"text-delta","id":7,"delta":"a"}`,
		`{"type":"text-delta","id":"t","delta":5}`,
		`{"type":"tool-input-start","id":3,"toolName":9}`,
		`{"type":"tool-input-delta","id":3,"delta":{"a":1}}`,
		`5`,
		`[1]`,
		`{"type":"finish","finishReason":7,"usage":"x"}`,
		`{"type":"finish","finishReason":{"unified":"stop","raw":"stop"},"usage":{"inputTokens":{"total":3},"outputTokens":{"total":2}}}`,
	)
	if err != nil {
		t.Fatalf("stream failed: %v (parts %v)", err, got)
	}
	want := []string{
		"text 7 a",
		"text t 5",
		"tool-input-start 3 9",
		"tool-input-delta 3 [object Object]",
		"finish ",
		"finish stop",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("parts:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	// Invalid JSON still fails the stream (TS controller.error).
	got, err = r87bintegGatewayParts(t, `{"type":"text-delta","id":"t","delta":"a"}`, `{bad`)
	if err == nil || !strings.Contains(err.Error(), "failed to parse stream chunk") || len(got) != 1 {
		t.Fatalf("invalid JSON: parts %v err %v", got, err)
	}
}

// Class sweep of provb CR 1: TS doGenerate parses the body with
// createJsonResponseHandler(z.any()) and spreads it. Recorded with bun on the
// TS doGenerate: the body below is returned as given (content text 5,
// toolCallId 7, finishReason 7, usage "x", warnings []), with no error. Go
// keeps both content parts (text "5", tool call id "7"); the mistyped
// finishReason, usage and providerMetadata read as zero. Before the sweep the
// whole call failed and a content part with a numeric id was dropped.
func TestR87bintegGatewayGenerateLenientDecode(t *testing.T) {
	res, err := decodeV4GenerateResult([]byte(`{"content":[{"type":"text","text":5},{"type":"tool-call","toolCallId":7,"toolName":"f","input":"{}"}],"finishReason":7,"usage":"x","providerMetadata":"p"}`))
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "5" || len(res.Content) != 2 || len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "7" || res.ToolCalls[0].ToolName != "f" {
		t.Fatalf("text %q content %#v tool calls %#v", res.Text, res.Content, res.ToolCalls)
	}
	if res.FinishReason != "" || res.Usage.InputTokens != nil || res.ProviderMetadata != nil || res.Warnings == nil {
		t.Fatalf("finish %q usage %+v metadata %v warnings %v", res.FinishReason, res.Usage, res.ProviderMetadata, res.Warnings)
	}
	if _, err := decodeV4GenerateResult([]byte(`{"content":[`)); err == nil {
		t.Fatal("invalid JSON decoded")
	}
}

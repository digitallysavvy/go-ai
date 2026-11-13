package types

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

// Round 99 gen tests (TS 2ebc78b654).

// JSON.stringify writes a non-finite number as null (ruling 4). The content
// parts' MarshalJSON go through jsonstr, which reaches only exported
// fields: with an unexported embedded alias the Infinity stayed and
// json.Marshal of the part failed.
func TestR99genContentPartMarshalNonFiniteIsNull(t *testing.T) {
	opts := map[string]interface{}{"p": map[string]interface{}{"v": math.Inf(1), "n": math.NaN()}}
	cases := []struct {
		name string
		part interface{}
		want string
	}{
		{"text", TextContent{Text: "x", ProviderOptions: opts}, `{"type":"text","text":"x","providerOptions":{"p":{"n":null,"v":null}}}`},
		{"reasoning", ReasoningContent{Text: "x", ProviderOptions: opts}, `{"type":"reasoning","text":"x","providerOptions":{"p":{"n":null,"v":null}}}`},
		{"image", ImageContent{MimeType: "image/png", ProviderOptions: opts}, `{"type":"image","image":null,"mimeType":"image/png","providerOptions":{"p":{"n":null,"v":null}}}`},
		{"file", FileContent{MediaType: "text/plain", ProviderOptions: opts}, `{"type":"file","fileData":{"type":""},"mediaType":"text/plain","providerOptions":{"p":{"n":null,"v":null}}}`},
		{"custom", CustomContent{Kind: "a.b", ProviderOptions: opts}, `{"type":"custom","kind":"a.b","providerOptions":{"p":{"n":null,"v":null}}}`},
		{"tool-error", ToolErrorContent{ToolCallID: "c", ToolName: "t", Error: map[string]interface{}{"v": math.Inf(-1)}, Err: errors.New("x")}, `{"type":"tool-error","toolCallId":"c","toolName":"t","error":{"v":null}}`},
		{"text block", TextContentBlock{Text: "x", ProviderOptions: opts}, `{"type":"text","text":"x","providerOptions":{"p":{"n":null,"v":null}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.part)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

package openresponses

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR96provbMetadataJSONStringify: Open Responses provider metadata built
// from provider values (item ids, extension items) is written like
// JSON.stringify: <, > and & stay literal (main ruling 4, write side).
func TestR96provbMetadataJSONStringify(t *testing.T) {
	const v = "<a&b>"
	item := &OutputItem{Type: "reasoning", ID: v}
	var calls []types.ToolCall
	var content []types.ContentPart
	pushOpenResponsesCall(&calls, &content, types.ToolCall{ID: "c", ToolName: "t", ProviderMetadata: map[string]interface{}{"open-responses": map[string]interface{}{"itemId": v}}})
	carrier := buildExtensionReplayCarrier("ext", ExtensionRecord{"type": "x:item", "id": v}, "open-responses")
	for name, raw := range map[string][]byte{
		"reasoning": openResponsesReasoningProviderMetadata("open-responses", item),
		"text":      openResponsesTextMetadataPayload("open-responses", item, nil),
		"toolCall":  content[0].(types.ToolCallContent).ProviderMetadata,
		"carrier":   carrier.ProviderMetadata,
		"reference": mergeExtensionReference(nil, "open-responses", map[string]interface{}{"id": "ext", "itemId": v}),
	} {
		if !strings.Contains(string(raw), v) {
			t.Errorf("%s: %s, want %q written literally", name, raw, v)
		}
	}
}

// TestR96provbStreamForbiddenPrototype: TS parseJsonEventStream fails a
// frame with a __proto__ / constructor.prototype key (secureJsonParse) as a
// JSONParseError part with finish reason error, also for a frame that is not
// an object (Go skipped such an array frame and decoded such an object).
func TestR96provbStreamForbiddenPrototype(t *testing.T) {
	for _, frame := range []string{
		`[{"__proto__":{}}]`,
		`{"type":"response.output_text.delta","item_id":"m","delta":"x","constructor":{"prototype":{}}}`,
	} {
		s := newOpenResponsesStream(io.NopCloser(strings.NewReader("data: "+frame+"\n\n")), nil)
		var sawErr bool
		for {
			c, err := s.Next()
			if err != nil {
				break
			}
			if c.Type == provider.ChunkTypeText {
				t.Errorf("%s: text delta emitted", frame)
			}
			if c.Type == provider.ChunkTypeError {
				var pe *providererrors.JSONParseError
				if !errors.As(c.Err, &pe) || !strings.Contains(c.Text, "forbidden prototype property") {
					t.Errorf("%s: error = %v (%T), want the JSONParseError", frame, c.Err, c.Err)
				}
				sawErr = true
			}
			if c.Type == provider.ChunkTypeFinish && c.FinishReason != types.FinishReasonError {
				t.Errorf("%s: finish = %s, want error", frame, c.FinishReason)
			}
		}
		if !sawErr {
			t.Errorf("%s: no error part", frame)
		}
	}
}

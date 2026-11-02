package ai

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Reviewer item 5 (r80): TS getToolPartInputSchemaInput uses hasOwnProperty,
// so `"inputSchemaInput": null` is validated (null against the tool input
// schema) instead of the part input, and it survives serialization.
func TestR80StreamApprovalInputSchemaInputNull(t *testing.T) {
	countTool := types.Tool{
		Name: "count",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"count": map[string]interface{}{"type": "string"}},
			"required":   []interface{}{"count"},
		},
	}
	_, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
		Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
			{"type":"tool-count","toolCallId":"call-1","state":"approval-responded","input":{"count":"3"},
			 "approval":{"id":"approval-1","approved":true,"inputSchemaInput":null}}
		]}]`),
		Tools: []types.Tool{countTool},
	})
	if err == nil {
		t.Fatal("null inputSchemaInput validated the part input instead of null")
	}
	if !strings.Contains(err.Error(), "messages[0].parts[0].input") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Absent key: the part input is validated (and passes).
	if _, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{
		Messages: json.RawMessage(`[{"id":"1","role":"assistant","parts":[
			{"type":"tool-count","toolCallId":"call-1","state":"approval-responded","input":{"count":"3"},
			 "approval":{"id":"approval-1","approved":true}}
		]}]`),
		Tools: []types.Tool{countTool},
	}); err != nil {
		t.Fatalf("absent inputSchemaInput: %v", err)
	}

	// Round trip keeps the null key; an absent key stays absent.
	for _, tc := range []struct{ in, want string }{
		{`{"id":"a","approved":true,"inputSchemaInput":null}`, `{"id":"a","approved":true,"inputSchemaInput":null}`},
		{`{"id":"a","approved":true}`, `{"id":"a","approved":true}`},
		{`{"id":"a","inputSchemaInput":{"x":1}}`, `{"id":"a","inputSchemaInput":{"x":1}}`},
	} {
		var a ToolUIPartApproval
		if err := json.Unmarshal([]byte(tc.in), &a); err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.want {
			t.Fatalf("round trip %s: got %s, want %s", tc.in, got, tc.want)
		}
	}
}

// r80streamTextStream runs StreamText and returns ToTextStream over its
// full stream (TS result.textStream).
func r80streamTextStream(t *testing.T, steps []provider.StreamChunk) []string {
	t.Helper()
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, steps), Prompt: "test-input", OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	out, errs := ToTextStream(context.Background(), r.FullStream())
	got := []string{}
	for s := range out {
		got = append(got, s)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	return got
}

// stream-text.test.ts 555 / 591 / 619: result.textStream sends the text
// deltas, drops empty ones, and leaves reasoning out.
func TestR80StreamTextStream(t *testing.T) {
	if got := r80streamTextStream(t, r71streamHelloWorld); !reflect.DeepEqual(got, []string{"Hello", ", ", "world!"}) {
		t.Fatalf("555 textStream = %q", got)
	}
	var withEmpty []provider.StreamChunk
	withEmpty = append(withEmpty, provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"})
	for _, d := range []string{"", "Hello", "", ", ", "", "world!", ""} {
		withEmpty = append(withEmpty, provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: d})
	}
	withEmpty = append(withEmpty, provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"}, r77streamFinishStop())
	if got := r80streamTextStream(t, withEmpty); !reflect.DeepEqual(got, []string{"Hello", ", ", "world!"}) {
		t.Fatalf("591 textStream = %q", got)
	}
	if got := r80streamTextStream(t, r74streamReasoningSteps()); !reflect.DeepEqual(got, []string{"Hi", " there!"}) {
		t.Fatalf("619 textStream = %q", got)
	}
}

// r80streamPartsJSON extends r77streamPartsJSON with the reasoning and
// tool-call fields and the text/reasoning providerMetadata of the TS
// snapshots.
func r80streamPartsJSON(parts []provider.StreamChunk) []map[string]interface{} {
	out := r77streamPartsJSON(parts, false)
	for i, p := range parts {
		m := out[i]
		switch p.Type {
		case provider.ChunkTypeReasoningStart, provider.ChunkTypeReasoningEnd:
			m["id"] = p.ID
		case provider.ChunkTypeReasoning:
			m["id"], m["text"] = p.ID, p.Reasoning
		case provider.ChunkTypeToolCall:
			c := p.ToolCall
			m["toolCallId"], m["toolName"], m["input"], m["title"] = c.ID, c.ToolName, c.Arguments, c.Title
			if c.ProviderExecuted {
				m["providerExecuted"] = true
			}
			if len(c.ProviderMetadata) > 0 {
				m["providerMetadata"] = c.ProviderMetadata
			}
		}
		switch p.Type {
		case provider.ChunkTypeReasoningStart, provider.ChunkTypeReasoningEnd, provider.ChunkTypeReasoning,
			provider.ChunkTypeTextStart, provider.ChunkTypeTextEnd, provider.ChunkTypeText:
			if len(p.ProviderMetadata) > 0 {
				m["providerMetadata"] = json.RawMessage(p.ProviderMetadata)
			}
		}
	}
	return out
}

const r80streamFinishParts = `{"type":"finish-step","finishReason":"stop","rawFinishReason":"stop","response":{"id":"id-0","modelId":"mock-model-id"},"usage":{"inputTokens":3,"outputTokens":10,"totalTokens":13},"hasTimeBetweenOutputChunks":%s},
	{"type":"finish","finishReason":"stop","rawFinishReason":"stop","totalUsage":{"inputTokens":3,"outputTokens":10,"totalTokens":13}}`

// stream-text.test.ts 814: result.stream > should send reasoning deltas
// (modelWithReasoning inline snapshot: every reasoning block in order, the
// empty delta that carries a signature kept, interleaved blocks 4/5 ended
// with their own metadata, timeBetweenOutputChunksMs set).
func TestR80StreamFullStreamReasoningDeltas(t *testing.T) {
	_, parts := r73streamParts(t, StreamTextOptions{Model: r71streamModel(nil, r74streamReasoningSteps()), Prompt: "prompt"})
	sig1 := `{"testProvider":{"signature":"1234567890"}}`
	sig2 := `{"testProvider":{"signature":"0987654321"}}`
	r75streamAssertJSON(t, "parts", r80streamPartsJSON(parts), `[{"type":"start"},{"type":"start-step"},
		{"type":"reasoning-start","id":"1"},
		{"type":"reasoning-delta","id":"1","text":"I will open the conversation"},
		{"type":"reasoning-delta","id":"1","text":" with witty banter."},
		{"type":"reasoning-delta","id":"1","text":"","providerMetadata":`+sig1+`},
		{"type":"reasoning-end","id":"1"},
		{"type":"reasoning-start","id":"2","providerMetadata":{"testProvider":{"redactedData":"redacted-reasoning-data"}}},
		{"type":"reasoning-end","id":"2"},
		{"type":"reasoning-start","id":"3"},
		{"type":"reasoning-delta","id":"3","text":" Once the user has relaxed,"},
		{"type":"reasoning-delta","id":"3","text":" I will pry for valuable information."},
		{"type":"reasoning-end","id":"3","providerMetadata":`+sig1+`},
		{"type":"reasoning-start","id":"4","providerMetadata":`+sig1+`},
		{"type":"reasoning-delta","id":"4","text":" I need to think about"},
		{"type":"reasoning-delta","id":"4","text":" this problem carefully."},
		{"type":"reasoning-start","id":"5","providerMetadata":`+sig1+`},
		{"type":"reasoning-delta","id":"5","text":" The best solution"},
		{"type":"reasoning-delta","id":"5","text":" requires careful"},
		{"type":"reasoning-delta","id":"5","text":" consideration of all factors."},
		{"type":"reasoning-end","id":"4","providerMetadata":`+sig2+`},
		{"type":"reasoning-end","id":"5","providerMetadata":`+sig2+`},
		{"type":"text-start","id":"1"},
		{"type":"text-delta","id":"1","text":"Hi"},
		{"type":"text-delta","id":"1","text":" there!"},
		{"type":"text-end","id":"1","providerMetadata":`+sig2+`},
		`+strings.Replace(r80streamFinishParts, "%s", "true", 1)+`]`)
}

// stream-text.test.ts 1734: result.stream > should send tool calls (the
// wire tools and tool choice, the parsed input, the call's provider
// metadata and the tool title; a tool without execute ends the run).
func TestR80StreamFullStreamToolCalls(t *testing.T) {
	var got *provider.GenerateOptions
	model := r71streamModel(nil, []provider.StreamChunk{
		{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id"}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "value" }`, ProviderMetadata: map[string]interface{}{"testProvider": map[string]interface{}{"signature": "sig"}}}},
		r77streamFinishStop(),
	})
	inner := model.DoStreamFunc
	model.DoStreamFunc = func(ctx context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
		got = o
		return inner(ctx, o)
	}
	_, parts := r73streamParts(t, StreamTextOptions{
		Model: model, Prompt: "test-input",
		Tools:      []types.Tool{{Name: "tool1", Title: "Tool 1", Parameters: r71streamValueSchema}},
		ToolChoice: types.RequiredToolChoice(),
	})
	if got == nil || len(got.Tools) != 1 || got.Tools[0].Name != "tool1" || !reflect.DeepEqual(got.Tools[0].Parameters, r71streamValueSchema) {
		t.Fatalf("tools = %+v", got)
	}
	if got.ToolChoice.Type != types.ToolChoiceRequired {
		t.Fatalf("tool choice = %+v", got.ToolChoice)
	}
	r75streamAssertJSON(t, "parts", r80streamPartsJSON(parts), `[{"type":"start"},{"type":"start-step"},
		{"type":"tool-call","toolCallId":"call-1","toolName":"tool1","input":{"value":"value"},"title":"Tool 1","providerMetadata":{"testProvider":{"signature":"sig"}}},
		`+strings.Replace(r80streamFinishParts, "%s", "false", 1)+`]`)
}

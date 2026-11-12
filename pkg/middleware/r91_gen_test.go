package middleware

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Round 91 gen tests (TS 2ebc78b654).

func r91genStream(t *testing.T, mw *LanguageModelMiddleware, model *mockLanguageModel) []*provider.StreamChunk {
	t.Helper()
	w := WrapLanguageModel(model, []*LanguageModelMiddleware{mw}, nil, nil)
	s, err := w.DoStream(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			return out
		}
		out = append(out, c)
	}
}

// core3-10: ExtractJSONMiddleware emits {type: 'text-delta', id, delta}
// only, not the triggering delta's metadata (extract-json-middleware.ts
// 183-188).
func TestR91genExtractJSONDeltaWithoutMetadata(t *testing.T) {
	chunks := r91genStream(t, ExtractJSONMiddleware(nil), &mockLanguageModel{stream: &mockTextStream{chunks: []*provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: `{"a":1}`, ProviderMetadata: json.RawMessage(`{"x":{"y":1}}`)},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
	}}})
	found := false
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeText {
			found = true
			if c.ProviderMetadata != nil {
				t.Errorf("delta %q carries provider metadata %s", c.Text, c.ProviderMetadata)
			}
		}
	}
	if !found {
		t.Fatalf("no text delta in %v", chunks)
	}
}

// core3-11: the prefix phase compares the JS (UTF-16) buffer length with 3,
// so "`é" (2 UTF-16 units, 3 UTF-8 bytes) keeps buffering: TS emits the
// block only at text-end, after a chunk that arrived in between.
func TestR91genExtractJSONPrefixUsesJSLength(t *testing.T) {
	chunks := r91genStream(t, ExtractJSONMiddleware(nil), &mockLanguageModel{stream: &mockTextStream{chunks: []*provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "`é"},
		{Type: provider.ChunkTypeReasoning, ID: "r", Reasoning: "x"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
	}}})
	var seq []string
	for _, c := range chunks {
		seq = append(seq, string(c.Type)+":"+c.Text)
	}
	want := []string{string(provider.ChunkTypeReasoning) + ":", string(provider.ChunkTypeTextStart) + ":", string(provider.ChunkTypeText) + ":`é", string(provider.ChunkTypeTextEnd) + ":"}
	if len(seq) != len(want) {
		t.Fatalf("chunks = %v, want %v", seq, want)
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("chunks = %v, want %v", seq, want)
		}
	}
}

// core3-14: the empty-delimiter InvalidArgumentError value is TS tagName,
// the {opening, closing} object (extract-reasoning-middleware.ts:33-38).
func TestR91genExtractReasoningEmptyDelimiterValue(t *testing.T) {
	defer func() {
		r := recover()
		e, ok := r.(*providererrors.InvalidArgumentError)
		if !ok {
			t.Fatalf("panic = %v", r)
		}
		v, ok := e.Value.(map[string]interface{})
		if !ok || v["opening"] != "" || v["closing"] != "x" || len(v) != 2 {
			t.Fatalf("Value = %#v, want {opening: \"\", closing: \"x\"}", e.Value)
		}
	}()
	ExtractReasoningMiddleware(&ExtractReasoningOptions{TagName: "think", Delimiters: &ReasoningDelimiters{Opening: "", Closing: "x"}})
}

// core3-15: SimulateStreamingMiddleware enqueues every tool-call content
// part, also one without a flat ToolCalls entry (TS 'should simulate
// streaming with tool calls', simulate-streaming-middleware.ts:67-70).
func TestR91genSimulateStreamingToolCallWithoutFlatEntry(t *testing.T) {
	chunks := r91genStream(t, SimulateStreamingMiddleware(), &mockLanguageModel{generateResult: &types.GenerateResult{
		Content: []types.ContentPart{
			types.TextContent{Text: "a"},
			types.ToolCallContent{ToolCallID: "tool-1", ToolName: "t", Input: `{"value":"x"}`},
		},
		FinishReason: types.FinishReasonToolCalls,
	}})
	var call *types.ToolCall
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeToolCall {
			call = c.ToolCall
		}
	}
	if call == nil || call.ID != "tool-1" || call.ToolName != "t" || call.RawArguments != `{"value":"x"}` || call.Arguments["value"] != "x" {
		t.Fatalf("tool call = %+v", call)
	}
}

package ai

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Replays stream-text.test.ts describe('result.toUIMessageStream') snapshots.

func uiModel(chunks ...provider.StreamChunk) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(append([]provider.StreamChunk(nil), chunks...)), nil
		},
	}
}

func tsUIParts(t *testing.T, r *StreamTextResult, opts ...UIMessageStreamResultOptions) []map[string]interface{} {
	t.Helper()
	ch, errs := r.ToUIMessageStream(context.Background(), opts...)
	var out []map[string]interface{}
	for c := range ch {
		// normalize through JSON so numbers and nested maps compare like the TS snapshots
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]interface{}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	if err := <-errs; err != nil {
		t.Fatalf("ui stream error: %v", err)
	}
	return out
}


func TestTSReplay_ToUIMessageStream(t *testing.T) {
	meta := func(sig string) json.RawMessage {
		return json.RawMessage(`{"testProvider":{"signature":"` + sig + `"}}`)
	}
	finishStop := provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)}
	text := []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, World!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		finishStop,
	}
	m := func(kv ...interface{}) map[string]interface{} {
		out := map[string]interface{}{}
		for i := 0; i < len(kv); i += 2 {
			out[kv[i].(string)] = kv[i+1]
		}
		return out
	}
	cases := []struct {
		name   string
		model  *testutil.MockLanguageModel
		opts   UIMessageStreamResultOptions
		tools  []types.Tool
		stream bool
		want   []map[string]interface{}
	}{
		{
			name: "plain text", model: uiModel(
				provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
				provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
				provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: ", "},
				provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "world!"},
				provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
				finishStop),
			want: []map[string]interface{}{
				m("type", "start"), m("type", "start-step"),
				m("type", "text-start", "id", "1"),
				m("type", "text-delta", "id", "1", "delta", "Hello"),
				m("type", "text-delta", "id", "1", "delta", ", "),
				m("type", "text-delta", "id", "1", "delta", "world!"),
				m("type", "text-end", "id", "1"),
				m("type", "finish-step"), m("type", "finish", "finishReason", "stop"),
			},
		},
		{
			name: "provider metadata on reasoning and text parts", model: uiModel(
				provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: "r1", ProviderMetadata: meta("r1")},
				provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: "r1", Text: "Hello", ProviderMetadata: meta("r2")},
				provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: "r1", Text: ", ", ProviderMetadata: meta("r3")},
				provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "r1", ProviderMetadata: meta("r4")},
				provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1", ProviderMetadata: meta("1")},
				provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello", ProviderMetadata: meta("2")},
				provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1", ProviderMetadata: meta("5")},
				finishStop),
			want: []map[string]interface{}{
				m("type", "start"), m("type", "start-step"),
				m("type", "reasoning-start", "id", "r1", "providerMetadata", m("testProvider", m("signature", "r1"))),
				m("type", "reasoning-delta", "id", "r1", "delta", "Hello", "providerMetadata", m("testProvider", m("signature", "r2"))),
				m("type", "reasoning-delta", "id", "r1", "delta", ", ", "providerMetadata", m("testProvider", m("signature", "r3"))),
				m("type", "reasoning-end", "id", "r1", "providerMetadata", m("testProvider", m("signature", "r4"))),
				m("type", "text-start", "id", "1", "providerMetadata", m("testProvider", m("signature", "1"))),
				m("type", "text-delta", "id", "1", "delta", "Hello", "providerMetadata", m("testProvider", m("signature", "2"))),
				m("type", "text-end", "id", "1", "providerMetadata", m("testProvider", m("signature", "5"))),
				m("type", "finish-step"), m("type", "finish", "finishReason", "stop"),
			},
		},
		{
			name: "tool input stream and result", model: uiModel(
				provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ID: "call-1", ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1"}},
				provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: `{ "value":`},
				provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: ` "value" }`},
				provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ID: "call-1", ToolCall: &types.ToolCall{ID: "call-1"}},
				provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}}},
				finishStop),
			tools: []types.Tool{func() types.Tool {
				tl := tool1()
				tl.Execute = func(_ context.Context, a map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
					return a["value"].(string) + "-result", nil
				}
				return tl
			}()},
			want: []map[string]interface{}{
				m("type", "start"), m("type", "start-step"),
				m("type", "tool-input-start", "toolCallId", "call-1", "toolName", "tool1"),
				m("type", "tool-input-delta", "toolCallId", "call-1", "inputTextDelta", `{ "value":`),
				m("type", "tool-input-delta", "toolCallId", "call-1", "inputTextDelta", ` "value" }`),
				m("type", "tool-input-available", "toolCallId", "call-1", "toolName", "tool1", "input", m("value", "value")),
				m("type", "tool-output-available", "toolCallId", "call-1", "output", "value-result"),
				m("type", "finish-step"), m("type", "finish", "finishReason", "stop"),
			},
		},
		{
			name: "sources", opts: UIMessageStreamResultOptions{SendSources: ptrBool(true)}, model: uiModel(
				provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "url", ID: "123", URL: "https://example.com", Title: "Example"}},
				provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
				provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello!"},
				provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
				provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "url", ID: "456", URL: "https://example.com/2", Title: "Example 2"}},
				finishStop),
			want: []map[string]interface{}{
				m("type", "start"), m("type", "start-step"),
				m("type", "source-url", "sourceId", "123", "url", "https://example.com", "title", "Example"),
				m("type", "text-start", "id", "1"), m("type", "text-delta", "id", "1", "delta", "Hello!"), m("type", "text-end", "id", "1"),
				m("type", "source-url", "sourceId", "456", "url", "https://example.com/2", "title", "Example 2"),
				m("type", "finish-step"), m("type", "finish", "finishReason", "stop"),
			},
		},
		{
			name: "files", model: uiModel(
				provider.StreamChunk{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "text/plain", Data: []byte("Hello World")}},
				provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
				provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hello!"},
				provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
				provider.StreamChunk{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "image/jpeg", Data: []byte("BAUG")}},
				finishStop),
			want: []map[string]interface{}{
				m("type", "start"), m("type", "start-step"),
				m("type", "file", "mediaType", "text/plain", "url", "data:text/plain;base64,SGVsbG8gV29ybGQ="),
				m("type", "text-start", "id", "1"), m("type", "text-delta", "id", "1", "delta", "Hello!"), m("type", "text-end", "id", "1"),
				m("type", "file", "mediaType", "image/jpeg", "url", "data:image/jpeg;base64,QkFVRw=="),
				m("type", "finish-step"), m("type", "finish", "finishReason", "stop"),
			},
		},
		{
			name: "sendFinish false", opts: UIMessageStreamResultOptions{SendFinish: ptrBool(false)}, model: uiModel(text...),
			want: []map[string]interface{}{
				m("type", "start"), m("type", "start-step"),
				m("type", "text-start", "id", "1"), m("type", "text-delta", "id", "1", "delta", "Hello, World!"), m("type", "text-end", "id", "1"),
				m("type", "finish-step"),
			},
		},
		{
			name: "sendStart false", opts: UIMessageStreamResultOptions{SendStart: ptrBool(false)}, model: uiModel(text...),
			want: []map[string]interface{}{
				m("type", "start-step"),
				m("type", "text-start", "id", "1"), m("type", "text-delta", "id", "1", "delta", "Hello, World!"), m("type", "text-end", "id", "1"),
				m("type", "finish-step"), m("type", "finish", "finishReason", "stop"),
			},
		},
		{
			name: "sendReasoning false", opts: UIMessageStreamResultOptions{SendReasoning: ptrBool(false)}, model: uiModel(
				provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: "1"},
				provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: "1", Text: "I will open the conversation"},
				provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "1"},
				provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "1"},
				provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: "Hi"},
				provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"},
				finishStop),
			want: []map[string]interface{}{
				m("type", "start"), m("type", "start-step"),
				m("type", "text-start", "id", "1"), m("type", "text-delta", "id", "1", "delta", "Hi"), m("type", "text-end", "id", "1"),
				m("type", "finish-step"), m("type", "finish", "finishReason", "stop"),
			},
		},
		{
			name: "error is masked by default", model: uiModel(provider.StreamChunk{Type: provider.ChunkTypeError, Err: errors.New("error")}),
			want: []map[string]interface{}{
				m("type", "start"), m("type", "start-step"),
				m("type", "error", "errorText", "An error occurred."),
				m("type", "finish-step"), m("type", "finish", "finishReason", "error"),
			},
		},
		{
			name: "custom error text", opts: UIMessageStreamResultOptions{OnError: func(err error) string { return "custom error message: " + err.Error() }},
			model: uiModel(provider.StreamChunk{Type: provider.ChunkTypeError, Err: errors.New("error")}),
			want: []map[string]interface{}{
				m("type", "start"), m("type", "start-step"),
				m("type", "error", "errorText", "custom error message: error"),
				m("type", "finish-step"), m("type", "finish", "finishReason", "error"),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := StreamText(context.Background(), StreamTextOptions{Model: tc.model, Prompt: "prompt", Tools: tc.tools, OnError: func(context.Context, error) {}})
			if err != nil {
				t.Fatal(err)
			}
			got := tsUIParts(t, r, tc.opts)
			if !reflect.DeepEqual(got, tc.want) {
				gb, _ := json.MarshalIndent(got, "", " ")
				wb, _ := json.MarshalIndent(tc.want, "", " ")
				t.Fatalf("ui parts\n got %s\nwant %s", gb, wb)
			}
		})
	}
}

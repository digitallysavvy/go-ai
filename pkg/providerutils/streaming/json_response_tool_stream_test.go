package streaming

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from packages/provider-utils/src/convert-json-response-tool-stream.test.ts
// (TS #21989).

type sliceStream struct {
	chunks []provider.StreamChunk
	i      int
}

func (s *sliceStream) Next() (*provider.StreamChunk, error) {
	if s.i >= len(s.chunks) {
		return nil, io.EOF
	}
	c := s.chunks[s.i]
	s.i++
	return &c, nil
}
func (s *sliceStream) Err() error   { return nil }
func (s *sliceStream) Close() error { return nil }

func convertJSONResponse(t *testing.T, chunks []provider.StreamChunk) []provider.StreamChunk {
	t.Helper()
	stream := NewJSONResponseToolStream(&sliceStream{chunks: chunks}, "json")
	var out []provider.StreamChunk
	for {
		c, err := stream.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, *c)
	}
}

var signatureMeta = json.RawMessage(`{"google":{"thoughtSignature":"signature"}}`)

func jsonResponseParts() []provider.StreamChunk {
	return []provider.StreamChunk{
		{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "response", ToolName: "json"}, ProviderMetadata: signatureMeta},
		{Type: provider.ChunkTypeToolInputDelta, ToolCall: &types.ToolCall{ID: "response"}, Text: `{"date":`},
		{Type: provider.ChunkTypeToolInputDelta, ToolCall: &types.ToolCall{ID: "response"}, Text: `"2031-06-17"}`},
		{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: "response"}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "response", ToolName: "json", RawArguments: `{"date":"2031-06-17"}`}, ProviderMetadata: signatureMeta},
	}
}

func finishToolCalls() provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool_calls"}
}

func TestJSONResponseToolStreamConvertsArgumentsAndFinishReason(t *testing.T) {
	out := convertJSONResponse(t, append(jsonResponseParts(), finishToolCalls()))
	wantTypes := []provider.ChunkType{provider.ChunkTypeTextStart, provider.ChunkTypeText, provider.ChunkTypeText, provider.ChunkTypeTextEnd, provider.ChunkTypeFinish}
	if len(out) != len(wantTypes) {
		t.Fatalf("got %d chunks: %#v", len(out), out)
	}
	for i, w := range wantTypes {
		if out[i].Type != w {
			t.Fatalf("chunk %d type = %s, want %s", i, out[i].Type, w)
		}
	}
	if out[0].ID != "response" || string(out[0].ProviderMetadata) != string(signatureMeta) || out[1].Text != `{"date":` || out[2].Text != `"2031-06-17"}` {
		t.Fatalf("unexpected chunks: %#v", out)
	}
	if string(out[3].ProviderMetadata) != string(signatureMeta) {
		t.Fatalf("text-end metadata = %s", out[3].ProviderMetadata)
	}
	if out[4].FinishReason != types.FinishReasonStop || out[4].RawFinishReason != "tool_calls" {
		t.Fatalf("finish = %#v", out[4])
	}
}

func TestJSONResponseToolStreamUsesCompleteInputWhenNoDeltas(t *testing.T) {
	out := convertJSONResponse(t, []provider.StreamChunk{
		{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "response", ToolName: "json"}},
		{Type: provider.ChunkTypeToolInputDelta, ToolCall: &types.ToolCall{ID: "response"}, Text: ""},
		{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: "response"}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "response", ToolName: "json", Arguments: map[string]interface{}{}}},
	})
	if len(out) != 4 || out[1].Text != "" || out[2].Text != "{}" || out[3].Type != provider.ChunkTypeTextEnd {
		t.Fatalf("out = %#v", out)
	}
}

func TestJSONResponseToolStreamPreservesApplicationCalls(t *testing.T) {
	application := []provider.StreamChunk{
		{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "application", ToolName: "resolveDate"}},
		{Type: provider.ChunkTypeToolInputDelta, ToolCall: &types.ToolCall{ID: "application"}, Text: "{}"},
		{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: "application"}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "application", ToolName: "resolveDate", RawArguments: "{}"}},
	}
	for _, order := range []string{"before", "after"} {
		var in []provider.StreamChunk
		if order == "before" {
			in = append(append(in, application...), jsonResponseParts()...)
		} else {
			in = append(append(in, jsonResponseParts()...), application...)
		}
		in = append(in, finishToolCalls())
		out := convertJSONResponse(t, in)

		var toolChunks []provider.ChunkType
		text := ""
		for _, c := range out {
			switch c.Type {
			case provider.ChunkTypeToolInputStart, provider.ChunkTypeToolInputDelta, provider.ChunkTypeToolInputEnd, provider.ChunkTypeToolCall:
				toolChunks = append(toolChunks, c.Type)
				if c.ToolCall != nil && c.ToolCall.ID != "application" {
					t.Fatalf("%s: unexpected tool chunk %#v", order, c)
				}
			case provider.ChunkTypeText:
				text += c.Text
			}
		}
		if len(toolChunks) != 4 || text != `{"date":"2031-06-17"}` {
			t.Fatalf("%s: toolChunks=%v text=%q", order, toolChunks, text)
		}
		last := out[len(out)-1]
		if last.Type != provider.ChunkTypeFinish || last.FinishReason != types.FinishReasonToolCalls {
			t.Fatalf("%s: finish = %#v, want unchanged tool-calls", order, last)
		}
	}
}

func TestJSONResponseToolStreamPreservesIncompleteFinish(t *testing.T) {
	in := append(jsonResponseParts(), provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonLength, RawFinishReason: "MAX_TOKENS"})
	out := convertJSONResponse(t, in)
	if last := out[len(out)-1]; last.FinishReason != types.FinishReasonLength {
		t.Fatalf("finish = %#v", last)
	}
}

func TestJSONResponseToolStreamOmitsConversationalText(t *testing.T) {
	reasoning := provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: "reasoning", Reasoning: "Thinking."}
	errChunk := provider.StreamChunk{Type: provider.ChunkTypeError, Text: "provider error"}
	out := convertJSONResponse(t, []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "conversation"},
		{Type: provider.ChunkTypeText, ID: "conversation", Text: "Here is the answer."},
		{Type: provider.ChunkTypeTextEnd, ID: "conversation"},
		reasoning,
		errChunk,
	})
	if len(out) != 2 || out[0].Type != provider.ChunkTypeReasoning || out[1].Type != provider.ChunkTypeError {
		t.Fatalf("out = %#v", out)
	}
}

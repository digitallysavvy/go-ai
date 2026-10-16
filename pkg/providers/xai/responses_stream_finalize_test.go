package xai

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func drainXAIResponses(t *testing.T, sse string) []*provider.StreamChunk {
	t.Helper()
	s := newXAIResponsesStream(io.NopCloser(strings.NewReader(sse)))
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		out = append(out, c)
	}
}

func xaiChunkTypes(chunks []*provider.StreamChunk) []string {
	var out []string
	for _, c := range chunks {
		out = append(out, string(c.Type))
	}
	return out
}

// TS xai-responses doStream: text blocks are text-start / text-delta per
// `text-<item_id>`, every block ends in flush() right before `finish`, and
// response.completed only records the finish state, so a stream that ends
// without one still finishes (other, zero usage).
func TestXAIResponsesStreamTextBlocksAndFlush(t *testing.T) {
	chunks := drainXAIResponses(t, `data: {"type":"response.output_text.delta","item_id":"a","output_index":0,"content_index":0,"delta":"Hi"}

data: {"type":"response.output_text.delta","item_id":"a","output_index":0,"content_index":0,"delta":""}

data: {"type":"response.completed","response":{"object":"response","output":[],"status":"completed","usage":{"input_tokens":2,"output_tokens":3},"service_tier":"default"}}

`)
	got := strings.Join(xaiChunkTypes(chunks), ",")
	if got != "text-start,text,text,text-end,finish" {
		t.Fatalf("chunks = %s", got)
	}
	fin := chunks[len(chunks)-1]
	if fin.FinishReason != types.FinishReasonStop || fin.RawFinishReason != "completed" || *fin.Usage.InputTokens != 2 {
		t.Fatalf("finish = %+v", fin)
	}
	if !strings.Contains(string(fin.ProviderMetadata), `"serviceTier":"default"`) {
		t.Fatalf("metadata = %s", fin.ProviderMetadata)
	}

	// No terminal event at all.
	chunks = drainXAIResponses(t, `data: {"type":"response.output_text.delta","item_id":"a","output_index":0,"content_index":0,"delta":"Hi"}`+"\n\n")
	if got := strings.Join(xaiChunkTypes(chunks), ","); got != "text-start,text,text-end,finish" {
		t.Fatalf("no-terminal chunks = %s", got)
	}
	if chunks[3].FinishReason != types.FinishReasonOther {
		t.Fatalf("finish = %+v", chunks[3])
	}
}

// TS message output item: text that was not streamed as deltas is emitted
// whole (once per item), and its url_citation annotations become sources.
func TestXAIResponsesStreamMessageItemText(t *testing.T) {
	chunks := drainXAIResponses(t, `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","status":"in_progress","content":[]}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Full","annotations":[{"type":"url_citation","url":"https://x.test","title":"X"}]}]}}

`)
	got := strings.Join(xaiChunkTypes(chunks), ",")
	if got != "text-start,text,source,text-end,finish" {
		t.Fatalf("chunks = %s", got)
	}
	if chunks[1].Text != "Full" || chunks[1].ID != "text-m1" || chunks[2].SourceContent.URL != "https://x.test" {
		t.Fatalf("chunks = %+v %+v", chunks[1], chunks[2])
	}
}

// TS emits reasoning-start before the first reasoning-delta when the stream
// has reasoning_text.delta events without a summary_part.added.
func TestXAIResponsesStreamReasoningTextStartsBeforeDelta(t *testing.T) {
	chunks := drainXAIResponses(t, `data: {"type":"response.reasoning_text.delta","item_id":"r1","output_index":0,"content_index":0,"delta":"hm"}

data: {"type":"response.completed","response":{"object":"response","output":[],"status":"completed","usage":{"input_tokens":2,"output_tokens":3}}}

`)
	got := strings.Join(xaiChunkTypes(chunks), ",")
	if got != "reasoning-start,reasoning,finish" {
		t.Fatalf("chunks = %s", got)
	}
}

// TS maps response.completed by status alone; incomplete_details only drives
// response.incomplete.
func TestXAIResponsesStreamCompletedIgnoresIncompleteDetails(t *testing.T) {
	chunks := drainXAIResponses(t, `data: {"type":"response.completed","response":{"object":"response","output":[],"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":2,"output_tokens":3}}}

`)
	fin := chunks[len(chunks)-1]
	if fin.FinishReason != types.FinishReasonOther || fin.RawFinishReason != "incomplete" {
		t.Fatalf("finish = %v raw %q, want other/incomplete", fin.FinishReason, fin.RawFinishReason)
	}
}

// TS enqueues reasoning-start, then reasoning-end when an encrypted reasoning
// item completes without summary events.
func TestXAIResponsesStreamEncryptedReasoningStartsBeforeEnd(t *testing.T) {
	chunks := drainXAIResponses(t, `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_1","summary":[],"status":"completed","encrypted_content":"enc"}}

data: {"type":"response.completed","response":{"object":"response","output":[],"status":"completed"}}

`)
	got := strings.Join(xaiChunkTypes(chunks), ",")
	if got != "reasoning-start,reasoning-end,finish" {
		t.Fatalf("chunks = %s", got)
	}
}

// TS handles a function_call that only appears on output_item.done.
func TestXAIResponsesStreamFunctionCallDoneWithoutAdded(t *testing.T) {
	chunks := drainXAIResponses(t, `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"c1","name":"f","arguments":"{\"a\":1}"}}

data: {"type":"response.completed","response":{"object":"response","output":[],"status":"completed"}}

`)
	got := strings.Join(xaiChunkTypes(chunks), ",")
	if got != "tool-input-end,tool-call,finish" {
		t.Fatalf("chunks = %s", got)
	}
	if chunks[2].FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("finish = %v, want tool-calls", chunks[2].FinishReason)
	}
}

// TS xai-responses doStream enqueues reasoning-delta for every
// reasoning_summary_text.delta / reasoning_text.delta, including empty ones.
func TestXAIResponsesStreamEmptyReasoningDeltasEmitted(t *testing.T) {
	chunks := drainXAIResponses(t, `data: {"type":"response.reasoning_summary_part.added","item_id":"r1","output_index":0,"summary_index":0,"part":{"type":"summary_text","text":""}}

data: {"type":"response.reasoning_summary_text.delta","item_id":"r1","output_index":0,"summary_index":0,"delta":""}

data: {"type":"response.reasoning_text.delta","item_id":"r2","output_index":1,"content_index":0,"delta":""}

data: {"type":"response.completed","response":{"object":"response","output":[],"status":"completed","usage":{"input_tokens":2,"output_tokens":3}}}

`)
	got := strings.Join(xaiChunkTypes(chunks), ",")
	if !strings.HasPrefix(got, "reasoning-start,reasoning,reasoning-start,reasoning") {
		t.Fatalf("chunks = %s", got)
	}
}

package openai

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// Mirrors TS map-openai-responses-finish-reason.test.ts.
func TestMapResponsesFinishReason(t *testing.T) {
	reason := func(r string) *responses.IncompleteDetails { return &responses.IncompleteDetails{Reason: r} }
	cases := []struct {
		details         *responses.IncompleteDetails
		hasFunctionCall bool
		want            types.FinishReason
	}{
		{nil, false, types.FinishReasonStop},
		{nil, true, types.FinishReasonToolCalls},
		{reason("max_output_tokens"), true, types.FinishReasonLength},
		{reason("content_filter"), true, types.FinishReasonContentFilter},
		{reason("something_new"), false, types.FinishReasonOther},
		{reason("something_new"), true, types.FinishReasonToolCalls},
	}
	for _, c := range cases {
		if got := mapResponsesFinishReason(c.details, c.hasFunctionCall); got != c.want {
			t.Errorf("mapResponsesFinishReason(%+v, %v) = %q, want %q", c.details, c.hasFunctionCall, got, c.want)
		}
	}
}

func streamFinishReason(t *testing.T, sse string) types.FinishReason {
	t.Helper()
	stream := newResponsesStreamWithMetadata(io.NopCloser(strings.NewReader(sse)), false, "", "openai", nil)
	defer stream.Close() //nolint:errcheck
	var finish types.FinishReason
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeFinish {
			finish = chunk.FinishReason
		}
	}
	return finish
}

// Streaming used to report "stop" after a function call because the
// tool-call flag was never passed to the mapping.
func TestResponsesStream_FinishReasonToolCallsAfterFunctionCall(t *testing.T) {
	got := streamFinishReason(t, `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"weather"}}

data: {"type":"response.function_call_arguments.delta","output_index":0,"delta":"{}"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"weather","arguments":"{}"}}

data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":1,"output_tokens":1}}}

`)
	if got != types.FinishReasonToolCalls {
		t.Fatalf("finish reason = %q, want %q", got, types.FinishReasonToolCalls)
	}
}

func TestResponsesStream_FinishReasonStopWithoutFunctionCall(t *testing.T) {
	got := streamFinishReason(t, `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1"}}

data: {"type":"response.output_text.delta","output_index":0,"delta":"hi"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1"}}

data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":1,"output_tokens":1}}}

`)
	if got != types.FinishReasonStop {
		t.Fatalf("finish reason = %q, want %q", got, types.FinishReasonStop)
	}
}

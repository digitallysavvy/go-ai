package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func oneMessageSSE(id, text string) string {
	return fmt.Sprintf("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":%q,\"model\":\"claude-haiku-4-5\",\"content\":[],\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n", id) +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		fmt.Sprintf("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%q}}\n\n", text) +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
}

// TS emits one finish part per message_stop and keeps reading, so
// concatenated messages each get their own finish.
func TestStreamConcatenatedMessagesEmitOneFinishEach(t *testing.T) {
	model, closeFn := fixtureModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, oneMessageSSE("msg_1", "one")+oneMessageSSE("msg_2", "two"))
	})
	defer closeFn()
	s, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	finishes := 0
	for _, c := range drain(t, s) {
		switch c.Type {
		case provider.ChunkTypeText:
			text.WriteString(c.Text)
		case provider.ChunkTypeFinish:
			finishes++
			if c.FinishReason != types.FinishReasonStop {
				t.Fatalf("finish reason = %v", c.FinishReason)
			}
		}
	}
	if finishes != 2 || text.String() != "onetwo" {
		t.Fatalf("finishes = %d, text = %q", finishes, text.String())
	}
}

// TS always enqueues stream-start first, even without warnings.
func TestStreamAlwaysStartsWithStreamStart(t *testing.T) {
	model, closeFn := fixtureModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, oneMessageSSE("msg_1", "one"))
	})
	defer closeFn()
	s, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	chunks := drain(t, s)
	if len(chunks) == 0 || chunks[0].Type != provider.ChunkTypeStreamStart || len(chunks[0].Warnings) != 0 {
		t.Fatalf("first chunk = %+v", chunks[0])
	}
}

// TS finishes on message_stop even when no message_delta came before it, with
// finish reason `other`.
func TestStreamMessageStopWithoutDeltaFinishesOther(t *testing.T) {
	model, closeFn := fixtureModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude\",\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n"+
			"data: {\"type\":\"message_stop\"}\n\n")
	})
	defer closeFn()
	s, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var fin *provider.StreamChunk
	n := 0
	for _, c := range drain(t, s) {
		if c.Type == provider.ChunkTypeFinish {
			fin = c
			n++
		}
	}
	if n != 1 || fin.FinishReason != types.FinishReasonOther || fin.Usage == nil {
		t.Fatalf("finish = %+v (count %d)", fin, n)
	}
}

// TS keeps one stream-wide finish reason and usage: a message_delta with a null
// stop_reason still applies (reason `other`, usage updated), and a later
// message that never gets a message_delta finishes with the carried values.
func TestStreamFinishReasonAndUsageCarryAcrossMessages(t *testing.T) {
	model, closeFn := fixtureModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w,
			"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"c\",\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n"+
				"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":null},\"usage\":{\"output_tokens\":9}}\n\n"+
				"data: {\"type\":\"message_stop\"}\n\n"+
				"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_2\",\"model\":\"c\",\"stop_reason\":\"tool_use\",\"usage\":{\"input_tokens\":4,\"output_tokens\":0}}}\n\n"+
				"data: {\"type\":\"message_stop\"}\n\n")
	})
	defer closeFn()
	s, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	var fins []*provider.StreamChunk
	for _, c := range drain(t, s) {
		if c.Type == provider.ChunkTypeFinish {
			fins = append(fins, c)
		}
	}
	if len(fins) != 2 {
		t.Fatalf("finishes = %d", len(fins))
	}
	if fins[0].FinishReason != types.FinishReasonOther || *fins[0].Usage.OutputTokens != 9 {
		t.Fatalf("first finish = %v usage %+v", fins[0].FinishReason, fins[0].Usage)
	}
	// message_start.stop_reason sets the carried reason; output tokens carry over.
	if fins[1].FinishReason != types.FinishReasonToolCalls || fins[1].RawFinishReason != "tool_use" ||
		*fins[1].Usage.InputTokens != 4 || *fins[1].Usage.OutputTokens != 9 {
		t.Fatalf("second finish = %v/%q usage %+v", fins[1].FinishReason, fins[1].RawFinishReason, fins[1].Usage)
	}
}

// TS JSON.stringify leaves <, > and & alone; the JSON tool text and streamed
// tool input must not carry < escapes.
func TestJSONToolAndToolInputAreNotHTMLEscaped(t *testing.T) {
	model, closeFn := haikuModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"m","type":"message","role":"assistant","model":"c","content":[{"type":"tool_use","id":"t1","name":"json","input":{"html":"<b>&</b>"}}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":1}}`)
	})
	defer closeFn()
	res, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, ResponseFormat: jsonFormat(map[string]interface{}{"html": map[string]interface{}{"type": "string"}}, "html")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != `{"html":"<b>&</b>"}` {
		t.Fatalf("text = %q", res.Text)
	}
}

// TS finishes only on message_stop (the stream has no flush), so a body that
// ends after message_delta ends without a finish part.
func TestStreamEndsWithoutFinishWhenMessageStopIsMissing(t *testing.T) {
	model, closeFn := fixtureModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"c\",\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n"+
			"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n")
	})
	defer closeFn()
	s, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range drain(t, s) {
		if c.Type == provider.ChunkTypeFinish {
			t.Fatalf("finish emitted without message_stop: %+v", c)
		}
	}
}

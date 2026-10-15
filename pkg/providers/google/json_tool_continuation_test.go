package google

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The JSON response tool and long decode continuation compose: the response
// tool's call becomes the text, chatter in any hop is dropped, and the
// continuation request keeps the rewritten tools.

const (
	jsonToolHop1 = `{"candidates":[{"content":{"role":"model","parts":[{"text":"chatter"}]},"finishReason":"CONTINUATION","index":0,"continuationToken":"t1"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`
	jsonToolHop2 = `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"json","args":{"date":"2031-06-17"}}}]},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":15,"candidatesTokenCount":4,"totalTokenCount":19}}`
)

func TestJSONResponseToolWithContinuationGenerate(t *testing.T) {
	srv, bodies := scriptedServer(t, scriptedReply{body: jsonToolHop1}, scriptedReply{body: jsonToolHop2})
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gemini-2.5-flash")
	res, err := m.DoGenerate(context.Background(), jsonToolOpts(types.ToolChoice{}))
	if err != nil {
		t.Fatal(err)
	}
	b := bodies()
	if len(b) != 2 || b[1]["continuationToken"] != "t1" {
		t.Fatalf("bodies = %v", b)
	}
	for i, body := range b {
		names := functionDeclNames(t, body)
		if len(names) != 2 || names[1] != "json" {
			t.Errorf("request %d tools = %v", i, names)
		}
	}
	if res.Text != `{"date":"2031-06-17"}` {
		t.Errorf("Text = %q", res.Text)
	}
	if len(res.ToolCalls) != 0 || res.FinishReason != types.FinishReasonStop {
		t.Errorf("toolCalls=%v finish=%v", res.ToolCalls, res.FinishReason)
	}
	if *res.Usage.InputTokens != 25 {
		t.Errorf("input tokens = %d", *res.Usage.InputTokens)
	}
}

func TestJSONResponseToolWithContinuationStream(t *testing.T) {
	srv, bodies := scriptedServer(t,
		scriptedReply{sse: true, body: sseEvents(jsonToolHop1)},
		scriptedReply{sse: true, body: sseEvents(jsonToolHop2)},
	)
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gemini-2.5-flash")
	stream, err := m.DoStream(context.Background(), jsonToolOpts(types.ToolChoice{}))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	text := ""
	var finish types.FinishReason
	for {
		c, err := stream.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			break
		}
		switch c.Type {
		case provider.ChunkTypeText:
			text += c.Text
		case provider.ChunkTypeToolCall:
			t.Fatalf("unexpected tool call %#v", c)
		case provider.ChunkTypeFinish:
			finish = c.FinishReason
		}
	}
	if text != `{"date":"2031-06-17"}` || finish != types.FinishReasonStop {
		t.Fatalf("text=%q finish=%q", text, finish)
	}
	b := bodies()
	if len(b) != 2 || b[1]["continuationToken"] != "t1" {
		t.Fatalf("bodies = %v", b)
	}
	if names := functionDeclNames(t, b[1]); len(names) != 2 || names[1] != "json" {
		t.Errorf("continuation tools = %v", names)
	}
}

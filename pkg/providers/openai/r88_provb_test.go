package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// r88provbChatStream serves frames as an SSE body to a chat model and returns
// every stream part.
func r88provbChatStream(t *testing.T, frames []string, compat bool) []*provider.StreamChunk {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, f := range frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	cfg := Config{APIKey: "k", BaseURL: srv.URL}
	if compat {
		cfg.OpenAICompatible, cfg.Name = true, "test-provider"
	}
	m, err := New(cfg).ChatModel("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	st, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: chatTestPrompt()})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var out []*provider.StreamChunk
	for {
		c, err := st.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
}

// TS f6dbf04b5d (openai-chat-language-model.ts doStream): an empty
// `content: ""` delta is emitted only when the chunk has no other text delta;
// a refusal in the same chunk replaces it.
func TestR88provbChatStreamEmptyContentWithRefusal(t *testing.T) {
	parts := r88provbChatStream(t, []string{
		`{"id":"c","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"","refusal":"No."},"finish_reason":null}]}`,
		`{"id":"c","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	}, false)
	var deltas []string
	var finish *provider.StreamChunk
	for _, p := range parts {
		switch p.Type {
		case provider.ChunkTypeText:
			deltas = append(deltas, p.Text)
		case provider.ChunkTypeFinish:
			finish = p
		}
	}
	if len(deltas) != 1 || deltas[0] != "No." {
		t.Fatalf("text deltas = %q, want [\"No.\"]", deltas)
	}
	if finish == nil || finish.FinishReason != "content-filter" || finish.RawFinishReason != "stop" {
		t.Fatalf("finish = %+v, want content-filter / stop", finish)
	}

	// Without a refusal the empty content delta keeps its v0.5 behavior.
	parts = r88provbChatStream(t, []string{
		`{"id":"c","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		`{"id":"c","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	}, false)
	deltas = nil
	for _, p := range parts {
		if p.Type == provider.ChunkTypeText {
			deltas = append(deltas, p.Text)
		}
	}
	if len(deltas) != 1 || deltas[0] != "" {
		t.Fatalf("text deltas = %q, want [\"\"]", deltas)
	}
}

// TS 6b97088662 (openai-responses-language-model.test.ts "refusal" block):
// refusal content parts become text parts with {itemId, phase} metadata, the
// structured output text includes them, and the finish reason is
// content-filter unless a client tool call is present. These cases bypass
// the response schema (provg CR: the refusal member of the message content
// union); the stream cases replay through TestTSCasesResponses.
func TestR88provbResponsesRefusalParts(t *testing.T) {
	m := &ResponsesLanguageModel{}
	for _, tc := range []struct {
		name       string
		body       string
		texts      []string
		finish     types.FinishReason
		structured *string
		types      []string
	}{
		{
			name:   "output_text and refusal",
			body:   `{"id":"r","model":"gpt-4o","output":[{"id":"msg_refusal_mixed","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Here is what I can share.","annotations":[]},{"type":"refusal","refusal":"I can't share the rest."}]}],"usage":{"input_tokens":13,"output_tokens":7}}`,
			texts:  []string{"Here is what I can share.", "I can't share the rest."},
			finish: types.FinishReasonContentFilter,
		},
		{
			name:       "final answer refusal",
			body:       `{"id":"r","model":"gpt-4o","output":[{"id":"msg_refusal_final","type":"message","status":"completed","role":"assistant","phase":"final_answer","content":[{"type":"refusal","refusal":"I'm sorry, but I can't assist with that request."}]}],"usage":{"input_tokens":13,"output_tokens":7}}`,
			texts:      []string{"I'm sorry, but I can't assist with that request."},
			finish:     types.FinishReasonContentFilter,
			structured: r88provbStrPtr("I'm sorry, but I can't assist with that request."),
		},
		{
			name:   "refusal with a function call",
			body:   `{"id":"r","model":"gpt-4o","output":[{"id":"msg_refusal_function_call","type":"message","status":"completed","role":"assistant","content":[{"type":"refusal","refusal":"I can't do that, but I can check the weather."}]},{"type":"function_call","id":"fc_1","call_id":"call_1","name":"weather","arguments":"{\"location\":\"Paris\"}","status":"completed"}],"usage":{"input_tokens":13,"output_tokens":7}}`,
			texts:  []string{"I can't do that, but I can check the weather."},
			finish: types.FinishReasonToolCalls,
			types:  []string{"text", "tool-call"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var resp responses.ResponsesAPIResponse
			if err := json.Unmarshal([]byte(tc.body), &resp); err != nil {
				t.Fatal(err)
			}
			res, err := m.convertResponse(resp, true, "", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			var texts, kinds []string
			for _, p := range res.Content {
				switch v := p.(type) {
				case types.TextContent:
					texts = append(texts, v.Text)
					kinds = append(kinds, "text")
					meta, _ := v.ProviderOptions["openai"].(map[string]interface{})
					if meta["itemId"] == nil || meta["annotations"] != nil {
						t.Errorf("text metadata = %v, want itemId only", v.ProviderOptions)
					}
				default:
					kinds = append(kinds, "tool-call")
				}
			}
			if strings.Join(texts, "|") != strings.Join(tc.texts, "|") {
				t.Errorf("texts = %q, want %q", texts, tc.texts)
			}
			if tc.types != nil && strings.Join(kinds, ",") != strings.Join(tc.types, ",") {
				t.Errorf("part types = %v, want %v", kinds, tc.types)
			}
			if res.FinishReason != tc.finish || res.RawFinishReason != "" {
				t.Errorf("finish = %s / %q, want %s / undefined", res.FinishReason, res.RawFinishReason, tc.finish)
			}
			if tc.structured != nil {
				got := responsesStructuredOutputText(resp.Output)
				if got == nil || *got != *tc.structured {
					t.Errorf("structuredOutputText = %v, want %q", got, *tc.structured)
				}
			}
		})
	}
}

func r88provbStrPtr(s string) *string { return &s }

// TS 6b97088662: response.refusal.delta and response.refusal.done each mark
// the response as refused (content-filter with no incomplete reason).
func TestR88provbResponsesStreamRefusalFlag(t *testing.T) {
	head := []string{
		`{"type":"response.created","response":{"id":"r","created_at":1,"model":"gpt-4o","status":"in_progress"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}`,
	}
	tail := []string{
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","role":"assistant","content":[]}}`,
		`{"type":"response.completed","response":{"id":"r","created_at":1,"model":"gpt-4o","status":"completed","incomplete_details":null,"usage":{"input_tokens":1,"output_tokens":1}}}`,
	}
	for name, mid := range map[string][]string{
		"delta only": {`{"type":"response.refusal.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"No."}`},
		"done only":  {`{"type":"response.refusal.done","item_id":"msg_1","output_index":0,"content_index":0,"refusal":"No."}`},
	} {
		t.Run(name, func(t *testing.T) {
			frames := append(append(append([]string{}, head...), mid...), tail...)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				for _, f := range frames {
					_, _ = w.Write([]byte("data: " + f + "\n\n"))
				}
			}))
			defer srv.Close()
			m, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("gpt-4o")
			if err != nil {
				t.Fatal(err)
			}
			st, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: chatTestPrompt()})
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			var finish *provider.StreamChunk
			var text string
			for {
				c, err := st.Next()
				if err != nil {
					break
				}
				if c.Type == provider.ChunkTypeText {
					text += c.Text
				}
				if c.Type == provider.ChunkTypeFinish {
					finish = c
				}
			}
			if finish == nil || finish.FinishReason != types.FinishReasonContentFilter {
				t.Fatalf("finish = %+v, want content-filter", finish)
			}
			if name == "delta only" && text != "No." {
				t.Fatalf("text = %q, want the refusal delta", text)
			}
		})
	}
}

// TS 6b97088662 (openai-batch.test.ts "preserves refusal content parts and
// maps the finish reason to content-filter").
func TestR88provbBatchRefusal(t *testing.T) {
	res, berr := convertOpenAIBatchResponseBodyBytes(json.RawMessage(`{"id":"resp_1","created_at":1,"model":"gpt-4o","output":[{"type":"message","role":"assistant","id":"msg_123","phase":null,"content":[{"type":"refusal","refusal":"I'm sorry, I can't assist with that."}]}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	if berr != nil {
		t.Fatal(berr)
	}
	if len(res.Content) != 1 {
		t.Fatalf("content = %#v, want one text part", res.Content)
	}
	if tc, ok := res.Content[0].(types.TextContent); !ok || tc.Text != "I'm sorry, I can't assist with that." {
		t.Fatalf("content = %#v", res.Content)
	}
	if res.FinishReason != types.FinishReasonContentFilter || res.RawFinishReason != "" {
		t.Fatalf("finish = %s / %q", res.FinishReason, res.RawFinishReason)
	}
}

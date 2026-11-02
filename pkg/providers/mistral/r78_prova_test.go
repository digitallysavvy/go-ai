package mistral

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r78provamistralServer(t *testing.T, body string, sse bool) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sse {
			w.Header().Set("Content-Type", "text/event-stream")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TS createLanguageModelResponseMetadata: created != null ? new Date(created * 1000),
// so created: 0 is the epoch and a fractional created keeps its milliseconds.
func TestR78prova_mistralCreatedTimestamp(t *testing.T) {
	for _, tc := range []struct {
		created string
		want    time.Time
	}{
		{"0", time.Unix(0, 0)},
		{"1711115037.5", time.UnixMilli(1711115037500)},
		{"null", time.Time{}},
	} {
		body := `{"id":"x","object":"chat.completion","created":` + tc.created + `,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
		m, err := New(Config{APIKey: "k", BaseURL: r78provamistralServer(t, body, false)}).LanguageModel("m")
		if err != nil {
			t.Fatal(err)
		}
		res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatalf("created %s: %v", tc.created, err)
		}
		if res.ResponseMetadata == nil || !res.ResponseMetadata.Timestamp.Equal(tc.want) {
			t.Fatalf("generate created %s: metadata %+v, want timestamp %v", tc.created, res.ResponseMetadata, tc.want)
		}

		sse := "data: " + `{"id":"x","object":"chat.completion.chunk","created":` + tc.created + `,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}` + "\n\n" +
			"data: " + `{"id":"x","object":"chat.completion.chunk","created":` + tc.created + `,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
		m, err = New(Config{APIKey: "k", BaseURL: r78provamistralServer(t, sse, true)}).LanguageModel("m")
		if err != nil {
			t.Fatal(err)
		}
		stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
		if err != nil {
			t.Fatal(err)
		}
		var got *provider.ResponseMetadata
		for {
			c, err := stream.Next()
			if err != nil {
				break
			}
			if c.Type == provider.ChunkTypeError {
				t.Fatalf("stream created %s: error chunk %v", tc.created, c.Err)
			}
			if c.Type == provider.ChunkTypeResponseMetadata && got == nil {
				got = c.ResponseMetadata
			}
		}
		_ = stream.Close()
		if got == nil || !got.Timestamp.Equal(tc.want) {
			t.Fatalf("stream created %s: metadata %+v, want timestamp %v", tc.created, got, tc.want)
		}
	}
}

func r78provamistralDrain(t *testing.T, stream provider.TextStream) (string, int, provider.ChunkType) {
	t.Helper()
	defer stream.Close() //nolint:errcheck
	text, finishes, last := "", 0, provider.ChunkType("")
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeText {
			text += c.Text
		}
		if c.Type == provider.ChunkTypeFinish {
			finishes++
		}
		last = c.Type
	}
	return text, finishes, last
}

// TS parseJsonEventStream drops `data: [DONE]` and keeps reading: frames
// after it are processed and the finish comes at the end of the body.
func TestR78prova_mistralReadsPastDone(t *testing.T) {
	sse := "data: " + `{"id":"x","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"a"},"finish_reason":null}]}` + "\n\ndata: [DONE]\n\n" +
		"data: " + `{"id":"x","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"b"},"finish_reason":"stop"}]}` + "\n\n"
	m, err := New(Config{APIKey: "k", BaseURL: r78provamistralServer(t, sse, true)}).LanguageModel("m")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if text, finishes, last := r78provamistralDrain(t, stream); text != "ab" || finishes != 1 || last != provider.ChunkTypeFinish {
		t.Fatalf("chat: text %q, finishes %d, last %s; want \"ab\", 1 finish last", text, finishes, last)
	}

	p := conversationServer(t, "text/event-stream",
		"data: "+`{"type":"message.output.delta","id":"m1","content_index":0,"content":"a","model":"mistral-medium-latest"}`+"\n\ndata: [DONE]\n\n"+
			"data: "+`{"type":"message.output.delta","id":"m1","content_index":0,"content":"b"}`+"\n\n"+
			"data: "+`{"type":"conversation.response.done","usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`+"\n\n", nil)
	cstream, err := p.ConversationModel("mistral-medium-latest").DoStream(context.Background(), &provider.GenerateOptions{Prompt: userPrompt("hi")})
	if err != nil {
		t.Fatal(err)
	}
	if text, finishes, last := r78provamistralDrain(t, cstream); text != "ab" || finishes != 1 || last != provider.ChunkTypeFinish {
		t.Fatalf("conversation: text %q, finishes %d, last %s; want \"ab\", 1 finish last", text, finishes, last)
	}
}

const r78provaTSHeaders = "the TS test builds a provider with headers and checks the TS test user agent (ai-sdk-mistral/0.0.0-test); the harness builds a default provider"

// TestR78prova_TSCasesChatAsserts replays mistral-chat-language-model.test.ts
// extracted at e64ed82f27 with --asserts (plain assertions as well as
// snapshots).
func TestR78prova_TSCasesChatAsserts(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/chat_asserts.json.gz")
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "mistral-small-latest"
		}
		m, err := New(Config{APIKey: "k", BaseURL: base}).LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, map[string]string{
		"should pass headers": r78provaTSHeaders,
	})
}

// TestR78prova_TSCasesConversation replays
// mistral-conversation-language-model.test.ts (e64ed82f27, --asserts).
func TestR78prova_TSCasesConversation(t *testing.T) {
	tsfixture.ProviderToolHook = func(id, name string, args map[string]interface{}) (types.Tool, bool) {
		return types.Tool{Name: name, Type: "provider", ProviderID: id, ProviderArgs: args}, true
	}
	defer func() { tsfixture.ProviderToolHook = nil }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/conversation.json.gz")
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "mistral-small-latest"
		}
		return New(Config{APIKey: "test-api-key", BaseURL: base}).ConversationModel(id)
	}, map[string]string{
		"streams search execution, reasoning, text, references and usage":        r78provaPrepareStream,
		"streams incremental function calls alongside search":                    r78provaPrepareStream,
		"streams premium search without execution info and reports the model ID": r78provaPrepareStream,
	})
}

const r78provaPrepareStream = "the TS test builds its stream with the local prepareStream helper, which the extractor does not follow; ported by hand in TestR78prova_ConversationStreams"

// Hand ports of the mistral-conversation-language-model.test.ts tests the
// extractor cannot evaluate (prepareStream, spread tool lists).

var r78provaConvUsage = map[string]interface{}{"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30, "connector_tokens": 5, "connectors": map[string]interface{}{"web_search": 1}}

func r78provaConvServer(t *testing.T, responses ...string) (string, *[]map[string]interface{}) {
	t.Helper()
	var bodies []map[string]interface{}
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var b map[string]interface{}
		_ = json.Unmarshal(raw, &b)
		mu.Lock()
		n := len(bodies)
		bodies = append(bodies, b)
		mu.Unlock()
		resp := responses[len(responses)-1]
		if n < len(responses) {
			resp = responses[n]
		}
		if strings.HasPrefix(resp, "data: ") {
			w.Header().Set("Content-Type", "text/event-stream")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &bodies
}

func r78provaConvSSE(events ...interface{}) string {
	var b strings.Builder
	for _, e := range events {
		raw, _ := json.Marshal(e)
		b.WriteString("data: " + string(raw) + "\n\n")
	}
	return b.String()
}

func r78provaJSON(v interface{}) interface{} {
	raw, _ := json.Marshal(v)
	var out interface{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// r78provaMatch is a toMatchObject check over JSON values.
func r78provaMatch(want, got interface{}) bool {
	switch w := want.(type) {
	case map[string]interface{}:
		g, ok := got.(map[string]interface{})
		if !ok {
			return false
		}
		for k, v := range w {
			if !r78provaMatch(v, g[k]) {
				return false
			}
		}
		return true
	case []interface{}:
		g, ok := got.([]interface{})
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !r78provaMatch(w[i], g[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(want, got)
}

func r78provaConvStream(t *testing.T, base string, opts *provider.GenerateOptions) []interface{} {
	t.Helper()
	stream, err := New(Config{APIKey: "test-api-key", BaseURL: base}).ConversationModel("mistral-small-latest").DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	var chunks []*provider.StreamChunk
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		chunks = append(chunks, c)
	}
	parts, _ := r78provaJSON(tsfixture.StreamToTS(chunks)).([]interface{})
	return parts
}

func r78provaFind(parts []interface{}, typ string) interface{} {
	for _, p := range parts {
		if m, _ := p.(map[string]interface{}); m["type"] == typ {
			return p
		}
	}
	return nil
}

func r78provaSearchTools() []types.Tool {
	return []types.Tool{{Name: "search", Type: "provider", ProviderID: "mistral.web_search"}}
}

func r78provaWhoWon() types.Prompt {
	return types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Who won?"}}}}}
}

func TestR78prova_ConversationStreams(t *testing.T) {
	reference := map[string]interface{}{"type": "tool_reference", "tool": "web_search", "title": "Results", "url": "https://example.com/results"}
	usage := r78provaConvUsage
	done := map[string]interface{}{"type": "conversation.response.done", "usage": usage}

	t.Run("streams search execution, reasoning, text, references and usage", func(t *testing.T) {
		base, bodies := r78provaConvServer(t, r78provaConvSSE(
			map[string]interface{}{"type": "conversation.response.started", "conversation_id": "conversation-1", "created_at": "2026-01-01T00:00:00Z"},
			map[string]interface{}{"type": "tool.execution.started", "id": "search-1", "name": "web_search", "arguments": ""},
			map[string]interface{}{"type": "tool.execution.delta", "id": "search-1", "name": "web_search", "arguments": "latest "},
			map[string]interface{}{"type": "tool.execution.delta", "id": "search-1", "name": "web_search", "arguments": "results"},
			map[string]interface{}{"type": "tool.execution.done", "id": "search-1", "name": "web_search", "info": map[string]interface{}{"status": "success"}},
			map[string]interface{}{"type": "message.output.delta", "id": "message-1", "content": map[string]interface{}{"type": "thinking", "thinking": []interface{}{map[string]interface{}{"type": "text", "text": "Think"}}}},
			map[string]interface{}{"type": "message.output.delta", "id": "message-1", "content_index": 1, "content": "Spain "},
			map[string]interface{}{"type": "message.output.delta", "id": "message-1", "content_index": 1, "content": map[string]interface{}{"type": "text", "text": "won."}},
			map[string]interface{}{"type": "message.output.delta", "id": "message-1", "content_index": 2, "content": reference},
			done,
		))
		parts := r78provaConvStream(t, base, &provider.GenerateOptions{Prompt: r78provaWhoWon(), Tools: r78provaSearchTools()})
		var got []interface{}
		for _, p := range parts {
			got = append(got, p.(map[string]interface{})["type"])
		}
		want := []interface{}{"stream-start", "response-metadata", "tool-input-start", "tool-input-delta", "tool-input-end", "tool-call", "tool-result", "reasoning-start", "reasoning-delta", "reasoning-end", "text-start", "text-delta", "text-delta", "source", "text-end", "finish"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("part types = %v\nwant %v", got, want)
		}
		if tc := r78provaFind(parts, "tool-call"); !r78provaMatch(map[string]interface{}{"toolName": "search", "providerExecuted": true, "input": `{"arguments":"latest results"}`}, tc) {
			t.Errorf("tool-call = %v", tc)
		}
		if last := parts[len(parts)-1]; !r78provaMatch(r78provaJSON(map[string]interface{}{"type": "finish", "finishReason": map[string]interface{}{"unified": "stop"}, "usage": map[string]interface{}{"inputTokens": map[string]interface{}{"total": 10}, "outputTokens": map[string]interface{}{"total": 20}}}), last) {
			t.Errorf("finish = %v", last)
		}
		if b := (*bodies)[0]; !r78provaMatch(r78provaJSON(map[string]interface{}{"stream": true, "tools": []interface{}{map[string]interface{}{"type": "web_search"}}}), b) {
			t.Errorf("request body = %v", b)
		}
	})

	t.Run("streams incremental function calls alongside search", func(t *testing.T) {
		base, _ := r78provaConvServer(t, r78provaConvSSE(
			map[string]interface{}{"type": "function.call.delta", "tool_call_id": "function-1", "name": "weather", "arguments": `{"city":`, "output_index": 0},
			map[string]interface{}{"type": "function.call.delta", "tool_call_id": "function-1", "name": "weather", "arguments": `"Madrid"}`, "output_index": 0},
			done,
		))
		parts := r78provaConvStream(t, base, &provider.GenerateOptions{Prompt: r78provaWhoWon(), Tools: r78provaSearchTools()})
		if tc := r78provaFind(parts, "tool-call"); !r78provaMatch(map[string]interface{}{"toolCallId": "function-1", "toolName": "weather", "input": `{"city":"Madrid"}`}, tc) {
			t.Errorf("tool-call = %v", tc)
		}
		if last := parts[len(parts)-1]; !r78provaMatch(map[string]interface{}{"finishReason": map[string]interface{}{"unified": "tool-calls"}}, last) {
			t.Errorf("finish = %v", last)
		}
	})

	t.Run("streams premium search without execution info and reports the model ID", func(t *testing.T) {
		base, _ := r78provaConvServer(t, r78provaConvSSE(
			map[string]interface{}{"type": "tool.execution.started", "id": "search-1", "name": "web_search_premium", "arguments": "query", "function": "web_search"},
			map[string]interface{}{"type": "tool.execution.done", "id": "search-1", "name": "web_search_premium"},
			map[string]interface{}{"type": "message.output.delta", "id": "message-1", "model": "mistral-small-latest", "content": "Answer"},
			done,
		))
		parts := r78provaConvStream(t, base, &provider.GenerateOptions{Prompt: r78provaWhoWon(), Tools: []types.Tool{{Name: "news", Type: "provider", ProviderID: "mistral.web_search_premium"}}})
		if tc := r78provaFind(parts, "tool-call"); !r78provaMatch(map[string]interface{}{"providerMetadata": map[string]interface{}{"mistral": map[string]interface{}{"function": "web_search"}}}, tc) {
			t.Errorf("tool-call = %v", tc)
		}
		wantResult := map[string]interface{}{"type": "tool-result", "toolCallId": "search-1", "toolName": "news", "result": map[string]interface{}{}}
		wantMeta := map[string]interface{}{"type": "response-metadata", "modelId": "mistral-small-latest"}
		var hasResult, hasMeta bool
		for _, p := range parts {
			hasResult = hasResult || reflect.DeepEqual(p, wantResult)
			hasMeta = hasMeta || reflect.DeepEqual(p, wantMeta)
		}
		if !hasResult || !hasMeta {
			t.Errorf("parts %v: tool-result %v, response-metadata %v", parts, hasResult, hasMeta)
		}
		if last := parts[len(parts)-1]; !r78provaMatch(map[string]interface{}{"finishReason": map[string]interface{}{"unified": "stop"}}, last) {
			t.Errorf("finish = %v", last)
		}
	})

	for i, events := range [][]interface{}{
		{map[string]interface{}{"type": "conversation.response.error", "message": "Search failed", "code": 500}},
		{map[string]interface{}{"type": "message.output.delta", "id": "message-1", "content": 42}},
		{map[string]interface{}{"type": "tool.execution.started", "id": "search-1", "name": "web_search", "arguments": ""}, done},
		{map[string]interface{}{"type": "message.output.delta", "id": "message-1", "content": "Truncated"}},
	} {
		t.Run(fmt.Sprintf("reports failed or incomplete streams %d", i), func(t *testing.T) {
			base, _ := r78provaConvServer(t, r78provaConvSSE(events...))
			parts := r78provaConvStream(t, base, &provider.GenerateOptions{Prompt: r78provaWhoWon(), Tools: r78provaSearchTools()})
			if last := parts[len(parts)-1]; !r78provaMatch(map[string]interface{}{"finishReason": map[string]interface{}{"unified": "error"}}, last) {
				t.Errorf("parts = %v", parts)
			}
		})
	}

	t.Run("includes raw events when requested", func(t *testing.T) {
		base, _ := r78provaConvServer(t, r78provaConvSSE(done))
		parts := r78provaConvStream(t, base, &provider.GenerateOptions{Prompt: r78provaWhoWon(), Tools: r78provaSearchTools(), IncludeRawChunks: true})
		want := r78provaJSON(map[string]interface{}{"type": "raw", "rawValue": done})
		for _, p := range parts {
			if reflect.DeepEqual(p, want) {
				return
			}
		}
		t.Errorf("no raw part %v in %v", want, parts)
	})
}

func TestR78prova_ConversationGenerateHandPorts(t *testing.T) {
	execution := map[string]interface{}{"type": "tool.execution", "id": "search-1", "name": "web_search", "arguments": "latest results", "info": map[string]interface{}{"status": "success"}}
	answer := map[string]interface{}{"type": "message.output", "model": "mistral-small-latest", "content": []interface{}{map[string]interface{}{"type": "text", "text": "Spain won."}}}
	body := func(outputs ...interface{}) string {
		raw, _ := json.Marshal(map[string]interface{}{"conversation_id": "conversation-1", "outputs": outputs, "usage": r78provaConvUsage})
		return string(raw)
	}

	t.Run("supports premium search and forcing a renamed search tool", func(t *testing.T) {
		base, bodies := r78provaConvServer(t, body(execution, answer))
		m := New(Config{APIKey: "test-api-key", BaseURL: base}).ConversationModel("mistral-small-latest")
		_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
			Prompt:     r78provaWhoWon(),
			Tools:      append(r78provaSearchTools(), types.Tool{Name: "news", Type: "provider", ProviderID: "mistral.web_search_premium"}),
			ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "news"},
		})
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]interface{}{"tools": []interface{}{map[string]interface{}{"type": "web_search_premium"}}, "completion_args": map[string]interface{}{"tool_choice": "any"}}
		if b := (*bodies)[0]; !r78provaMatch(want, b) {
			t.Errorf("request body = %v", b)
		}
	})

	t.Run("supports function calls alongside search and replays their results", func(t *testing.T) {
		fnCall := map[string]interface{}{"type": "function.call", "tool_call_id": "function-1", "name": "weather", "arguments": map[string]interface{}{"city": "Madrid"}}
		base, bodies := r78provaConvServer(t, body(execution, fnCall))
		m := New(Config{APIKey: "test-api-key", BaseURL: base}).ConversationModel("mistral-small-latest")
		tools := append(r78provaSearchTools(), types.Tool{Name: "weather", Parameters: map[string]interface{}{"type": "object"}})
		first, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: r78provaWhoWon(), Tools: tools})
		if err != nil {
			t.Fatal(err)
		}
		if first.FinishReason != types.FinishReasonToolCalls {
			t.Errorf("finish reason = %v", first.FinishReason)
		}
		got := r78provaJSON(tsfixture.GenerateContentToTS(first))
		content, _ := got.([]interface{})
		want := map[string]interface{}{"type": "tool-call", "toolCallId": "function-1", "toolName": "weather", "input": `{"city":"Madrid"}`}
		if len(content) < 3 || !reflect.DeepEqual(content[2], want) {
			t.Fatalf("content = %v", content)
		}
		searchCall := first.ToolCalls[0]
		prompt := types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Who won?"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: searchCall.ID, ToolName: searchCall.ToolName, Arguments: searchCall.Arguments, ProviderExecuted: true, ProviderOptions: searchCall.ProviderMetadata},
				types.ToolResultContent{ToolCallID: searchCall.ID, ToolName: searchCall.ToolName, Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"info": execution["info"]}}},
				types.TextContent{Text: "Spain won."},
			}},
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Who did they beat?"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "function-1", ToolName: "weather", Input: `{"city":"Madrid"}`}}},
			{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "function-1", ToolName: "weather", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"temperature": 20}}}}},
		}}
		if _, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: prompt, Tools: tools}); err != nil {
			t.Fatal(err)
		}
		inputs, _ := (*bodies)[1]["inputs"].([]interface{})
		wantTail := r78provaJSON([]interface{}{
			map[string]interface{}{"type": "function.call", "tool_call_id": "function-1", "name": "weather", "arguments": `{"city":"Madrid"}`},
			map[string]interface{}{"type": "function.result", "tool_call_id": "function-1", "result": `{"temperature":20}`},
		})
		if len(inputs) < 2 || !reflect.DeepEqual(inputs[len(inputs)-2:], wantTail) {
			t.Errorf("inputs = %v", inputs)
		}
	})
}

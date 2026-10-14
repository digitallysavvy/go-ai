package mistral

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Remaining cases of TS mistral-conversation-language-model.test.ts,
// convert-to-mistral-conversation-inputs.test.ts and
// mistral-conversation-prepare-tools.test.ts (TS #22237).

type convRecorder struct {
	mu       sync.Mutex
	bodies   []map[string]interface{}
	headers  []http.Header
	response string
	ctype    string
}

func newConvRecorder(t *testing.T, ctype, response string) (*convRecorder, *Provider) {
	t.Helper()
	rec := &convRecorder{response: response, ctype: ctype}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]interface{}
		_ = json.Unmarshal(raw, &body)
		rec.mu.Lock()
		rec.bodies = append(rec.bodies, body)
		rec.headers = append(rec.headers, r.Header.Clone())
		resp, ct := rec.response, rec.ctype
		rec.mu.Unlock()
		w.Header().Set("Content-Type", ct)
		w.Header().Set("x-response", "response-value")
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return rec, New(Config{APIKey: "test-api-key", BaseURL: srv.URL, Headers: map[string]string{"x-provider": "provider-value"}})
}

func (r *convRecorder) set(ctype, response string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ctype, r.response = ctype, response
}

func (r *convRecorder) body(i int) map[string]interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bodies[i]
}

const (
	convUsage     = `{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30,"connector_tokens":5,"connectors":{"web_search":1}}`
	convExecution = `{"type":"tool.execution","id":"search-1","name":"web_search","arguments":"latest results","info":{"status":"success"}}`
	convReference = `{"type":"tool_reference","tool":"web_search","title":"Results","url":"https://example.com/results"}`
	convAnswer    = `{"type":"message.output","model":"mistral-small-latest","content":[{"type":"text","text":"Spain won."},` + convReference + `]}`
)

func convOutputs(outputs ...string) string {
	return `{"conversation_id":"conversation-1","outputs":[` + strings.Join(outputs, ",") + `],"usage":` + convUsage + `}`
}

func searchTool(name string) types.Tool {
	tl := WebSearch()
	tl.Name = name
	return tl
}

func premiumTool(name string) types.Tool {
	tl := WebSearchPremium()
	tl.Name = name
	return tl
}

func whoWon() types.Prompt { return userPrompt("Who won?") }

func jsonOut(v interface{}) *types.ToolResultOutput {
	return &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: v}
}

// followupMessages builds the TS `followup()` history from a generated
// provider-executed tool call.
func followupMessages(call types.ToolCallContent, info map[string]interface{}) []types.Message {
	var input interface{}
	_ = json.Unmarshal([]byte(call.Input), &input)
	return append(append([]types.Message{}, whoWon().Messages...),
		types.Message{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: call.ToolCallID, ToolName: call.ToolName, Input: call.Input, ProviderExecuted: true, ProviderMetadata: call.ProviderMetadata},
			types.ToolResultContent{ToolCallID: call.ToolCallID, ToolName: call.ToolName, Output: jsonOut(map[string]interface{}{"info": info})},
			types.TextContent{Text: "Spain won."},
		}},
		userPrompt("Who did they beat?").Messages[0],
	)
}

func generateWith(t *testing.T, p *Provider, opts *provider.GenerateOptions) *types.GenerateResult {
	t.Helper()
	res, err := p.ConversationModel("mistral-small-latest").DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func streamWith(t *testing.T, p *Provider, opts *provider.GenerateOptions) []*provider.StreamChunk {
	t.Helper()
	stream, err := p.ConversationModel("mistral-small-latest").DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	return drainConversation(t, stream)
}

func chunkTypes(chunks []*provider.StreamChunk) []provider.ChunkType {
	var out []provider.ChunkType
	for _, c := range chunks {
		out = append(out, c.Type)
	}
	return out
}

func findChunk(chunks []*provider.StreamChunk, typ provider.ChunkType) *provider.StreamChunk {
	for _, c := range chunks {
		if c.Type == typ {
			return c
		}
	}
	return nil
}

func TestConversationForwardsHeadersAndRequestBody(t *testing.T) {
	for _, method := range []string{"generate", "stream"} {
		t.Run(method, func(t *testing.T) {
			var rec *convRecorder
			var p *Provider
			if method == "generate" {
				rec, p = newConvRecorder(t, "application/json", convOutputs(convAnswer))
			} else {
				rec, p = newConvRecorder(t, "text/event-stream", sse(`{"type":"conversation.response.done","usage":`+convUsage+`}`))
			}
			opts := &provider.GenerateOptions{Prompt: whoWon(), Tools: []types.Tool{searchTool("search")}, Headers: map[string]string{"x-call": "call-value"}}
			if method == "generate" {
				res := generateWith(t, p, opts)
				if res.Text != "Spain won." || res.FinishReason != types.FinishReasonStop {
					t.Errorf("result = %#v", res)
				}
				if res.ResponseHeaders["X-Response"] != "response-value" && res.ResponseHeaders["x-response"] != "response-value" {
					t.Errorf("response headers = %v", res.ResponseHeaders)
				}
			} else {
				chunks := streamWith(t, p, opts)
				if chunks[0].Type != provider.ChunkTypeStreamStart || len(chunks[0].Warnings) != 0 {
					t.Errorf("first = %#v", chunks[0])
				}
				if last := chunks[len(chunks)-1]; last.Type != provider.ChunkTypeFinish || last.FinishReason != types.FinishReasonStop {
					t.Errorf("last = %#v", last)
				}
			}
			h := rec.headers[0]
			if h.Get("Authorization") != "Bearer test-api-key" || h.Get("x-provider") != "provider-value" || h.Get("x-call") != "call-value" {
				t.Errorf("headers = %v", h)
			}
		})
	}
}

func TestConversationUsesConversationsWithoutTools(t *testing.T) {
	rec, p := newConvRecorder(t, "application/json", convOutputs(convAnswer))
	model := p.ConversationModel("mistral-small-latest")
	if model.Provider() != "mistral.conversation" {
		t.Errorf("provider = %s", model.Provider())
	}
	generateWith(t, p, &provider.GenerateOptions{Prompt: whoWon()})
	want := map[string]interface{}{
		"model": "mistral-small-latest", "store": false,
		"inputs":          []interface{}{map[string]interface{}{"type": "message.input", "role": "user", "content": []interface{}{map[string]interface{}{"type": "text", "text": "Who won?"}}}},
		"completion_args": map[string]interface{}{},
	}
	if !reflect.DeepEqual(rec.body(0), want) {
		t.Errorf("body = %#v", rec.body(0))
	}
}

func TestConversationProviderToolsAreProviderExecuted(t *testing.T) {
	for id, tl := range map[string]types.Tool{"mistral.web_search": WebSearch(), "mistral.web_search_premium": WebSearchPremium()} {
		if tl.Type != "" && tl.Type != types.ToolTypeProviderDefined || tl.ProviderID != id || !tl.ProviderExecuted {
			t.Errorf("%s = %#v", id, tl)
		}
	}
}

func TestConversationGenerateMapsToolResultSourcesUsage(t *testing.T) {
	rec, p := newConvRecorder(t, "application/json", convOutputs(convExecution, convAnswer))
	res := generateWith(t, p, &provider.GenerateOptions{Prompt: whoWon(), Tools: []types.Tool{searchTool("search")}})
	if len(res.Content) != 4 {
		t.Fatalf("content = %#v", res.Content)
	}
	call := res.Content[0].(types.ToolCallContent)
	if call.ToolCallID != "search-1" || call.ToolName != "search" || call.Input != `{"arguments":"latest results"}` || !call.ProviderExecuted {
		t.Errorf("call = %#v", call)
	}
	if string(call.ProviderMetadata) != `{"mistral":{"name":"web_search","type":"tool.execution"}}` {
		t.Errorf("metadata = %s", call.ProviderMetadata)
	}
	tr := res.Content[1].(types.ToolResultContent)
	if tr.ToolCallID != "search-1" || tr.ToolName != "search" || !reflect.DeepEqual(tr.Result, map[string]interface{}{"info": map[string]interface{}{"status": "success"}}) {
		t.Errorf("result = %#v", tr)
	}
	if txt := res.Content[2].(types.TextContent); txt.Text != "Spain won." {
		t.Errorf("text = %#v", txt)
	}
	src := res.Content[3].(types.SourceContent)
	if src.SourceType != "url" || src.ID == "" || src.URL != "https://example.com/results" || src.Title != "Results" {
		t.Errorf("source = %#v", src)
	}
	if res.FinishReason != types.FinishReasonStop {
		t.Errorf("finish = %v", res.FinishReason)
	}
	if *res.Usage.InputTokens != 10 || *res.Usage.OutputTokens != 20 || res.Usage.Raw["connector_tokens"] != 5.0 {
		t.Errorf("usage = %#v", res.Usage)
	}
	if res.ResponseMetadata.ID != "conversation-1" || res.ResponseMetadata.ModelID != "mistral-small-latest" {
		t.Errorf("response = %#v", res.ResponseMetadata)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v", res.Warnings)
	}
	wantTools := []interface{}{map[string]interface{}{"type": "web_search"}}
	if !reflect.DeepEqual(rec.body(0)["tools"], wantTools) {
		t.Errorf("tools = %#v", rec.body(0)["tools"])
	}
}

func TestConversationInstructionsAndCompletionSettings(t *testing.T) {
	rec, p := newConvRecorder(t, "application/json", convOutputs(convExecution, convAnswer))
	maxTokens, seed := 100, 1
	temp, topP, freq, pres := 0.3, 0.9, 0.1, 0.2
	high := types.ReasoningHigh
	generateWith(t, p, &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: append([]types.Message{
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "Be accurate."}}},
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "Be concise."}}},
		}, whoWon().Messages...)},
		Tools: []types.Tool{searchTool("search")}, MaxTokens: &maxTokens, Temperature: &temp, TopP: &topP,
		FrequencyPenalty: &freq, PresencePenalty: &pres, StopSequences: []string{"END"}, Seed: &seed,
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object"}},
		ToolChoice:     types.ToolChoice{Type: types.ToolChoiceRequired}, Reasoning: &high,
	})
	body := rec.body(0)
	if body["instructions"] != "Be accurate.\n\nBe concise." {
		t.Errorf("instructions = %v", body["instructions"])
	}
	ca := body["completion_args"].(map[string]interface{})
	for k, v := range map[string]interface{}{
		"max_tokens": 100.0, "temperature": 0.3, "top_p": 0.9, "frequency_penalty": 0.1, "presence_penalty": 0.2,
		"random_seed": 1.0, "tool_choice": "any", "reasoning_effort": "high",
	} {
		if ca[k] != v {
			t.Errorf("completion_args[%s] = %v, want %v", k, ca[k], v)
		}
	}
	if !reflect.DeepEqual(ca["stop"], []interface{}{"END"}) {
		t.Errorf("stop = %v", ca["stop"])
	}
	rf := ca["response_format"].(map[string]interface{})
	js, _ := rf["json_schema"].(map[string]interface{})
	if rf["type"] != "json_schema" || !reflect.DeepEqual(js["schema"], map[string]interface{}{"type": "object"}) {
		t.Errorf("response_format = %#v", rf)
	}
}

func TestConversationPremiumSearchAndForcedRenamedTool(t *testing.T) {
	rec, p := newConvRecorder(t, "application/json", convOutputs(convExecution, convAnswer))
	generateWith(t, p, &provider.GenerateOptions{
		Prompt: whoWon(), Tools: []types.Tool{searchTool("search"), premiumTool("news")},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "news"},
	})
	body := rec.body(0)
	if !reflect.DeepEqual(body["tools"], []interface{}{map[string]interface{}{"type": "web_search_premium"}}) ||
		body["completion_args"].(map[string]interface{})["tool_choice"] != "any" {
		t.Errorf("body = %#v", body)
	}
}

func TestConversationReplaysSearchExecutionsOnFollowUp(t *testing.T) {
	rec, p := newConvRecorder(t, "application/json", convOutputs(convExecution, convAnswer))
	tools := []types.Tool{searchTool("search")}
	first := generateWith(t, p, &provider.GenerateOptions{Prompt: whoWon(), Tools: tools})
	call := first.Content[0].(types.ToolCallContent)
	generateWith(t, p, &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: followupMessages(call, map[string]interface{}{"status": "success"})}, Tools: tools,
	})
	user := func(text string) map[string]interface{} {
		return map[string]interface{}{"type": "message.input", "role": "user", "content": []interface{}{map[string]interface{}{"type": "text", "text": text}}}
	}
	want := []interface{}{
		user("Who won?"),
		map[string]interface{}{"type": "function.call", "tool_call_id": "search-1", "name": "web_search", "arguments": "latest results"},
		map[string]interface{}{"type": "function.result", "tool_call_id": "search-1", "result": `{"status":"success"}`},
		map[string]interface{}{"type": "message.input", "role": "assistant", "content": []interface{}{map[string]interface{}{"type": "text", "text": "Spain won."}}},
		user("Who did they beat?"),
	}
	if got := rec.body(1)["inputs"]; !reflect.DeepEqual(got, want) {
		t.Errorf("inputs = %#v\nwant %#v", got, want)
	}

	// The search history still replays after the tool is removed.
	generateWith(t, p, &provider.GenerateOptions{Prompt: types.Prompt{Messages: followupMessages(call, map[string]interface{}{"status": "success"})}})
	second := rec.body(2)["inputs"].([]interface{})[1].(map[string]interface{})
	if second["type"] != "function.call" || second["name"] != "web_search" {
		t.Errorf("replay without tool = %#v", second)
	}

	// No history is retained on the shared model instance.
	generateWith(t, p, &provider.GenerateOptions{Prompt: userPrompt("Independent search"), Tools: tools})
	if got := rec.body(3)["inputs"].([]interface{}); len(got) != 1 {
		t.Errorf("shared instance inputs = %#v", got)
	}
}

func TestConversationPreservesExecutedFunctionAndRawResultForReplay(t *testing.T) {
	exec := `{"type":"tool.execution","id":"search-1","name":"web_search","function":"web_fetch","arguments":"{\"url\":\"https://example.com\"}","info":{"result":"[{\"text\":\"Retrieved page\"}]"}}`
	rec, p := newConvRecorder(t, "application/json", convOutputs(exec, convAnswer))
	tools := []types.Tool{searchTool("search")}
	first := generateWith(t, p, &provider.GenerateOptions{Prompt: whoWon(), Tools: tools})
	call := first.Content[0].(types.ToolCallContent)
	generateWith(t, p, &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: followupMessages(call, map[string]interface{}{"result": `[{"text":"Retrieved page"}]`})}, Tools: tools,
	})
	got := rec.body(1)["inputs"].([]interface{})[1:3]
	want := []interface{}{
		map[string]interface{}{"type": "function.call", "tool_call_id": "search-1", "name": "web_fetch", "arguments": `{"url":"https://example.com"}`},
		map[string]interface{}{"type": "function.result", "tool_call_id": "search-1", "result": `[{"text":"Retrieved page"}]`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("replay = %#v", got)
	}
}

func TestConversationFunctionCallsAlongsideSearch(t *testing.T) {
	fnCall := `{"type":"function.call","tool_call_id":"function-1","name":"weather","arguments":{"city":"Madrid"}}`
	rec, p := newConvRecorder(t, "application/json", convOutputs(convExecution, fnCall))
	mixed := []types.Tool{searchTool("search"), {Name: "weather", Parameters: map[string]interface{}{"type": "object"}}}
	first := generateWith(t, p, &provider.GenerateOptions{Prompt: whoWon(), Tools: mixed})
	if first.FinishReason != types.FinishReasonToolCalls {
		t.Errorf("finish = %v", first.FinishReason)
	}
	fc := first.Content[2].(types.ToolCallContent)
	if fc.ToolCallID != "function-1" || fc.ToolName != "weather" || fc.Input != `{"city":"Madrid"}` || fc.ProviderExecuted {
		t.Errorf("function call = %#v", fc)
	}
	msgs := append(followupMessages(first.Content[0].(types.ToolCallContent), map[string]interface{}{"status": "success"}),
		types.Message{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "function-1", ToolName: "weather", Input: `{"city":"Madrid"}`}}},
		types.Message{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "function-1", ToolName: "weather", Output: jsonOut(map[string]interface{}{"temperature": 20.0})}}},
	)
	generateWith(t, p, &provider.GenerateOptions{Prompt: types.Prompt{Messages: msgs}, Tools: mixed})
	inputs := rec.body(1)["inputs"].([]interface{})
	got := inputs[len(inputs)-2:]
	want := []interface{}{
		map[string]interface{}{"type": "function.call", "tool_call_id": "function-1", "name": "weather", "arguments": `{"city":"Madrid"}`},
		map[string]interface{}{"type": "function.result", "tool_call_id": "function-1", "result": `{"temperature":20}`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("replay = %#v", got)
	}
}

func TestConversationGenerateStringOutputReasoningAndDuplicateReferences(t *testing.T) {
	noURL := `{"type":"tool_reference","tool":"web_search","title":"Results","url":null}`
	body := `{"conversation_id":"conversation-1","outputs":[
		{"type":"message.output","content":"Hello"},
		{"type":"message.output","content":[{"type":"thinking","thinking":[{"type":"text","text":"Think"}]},` + convReference + `,` + convReference + `,` + noURL + `,{"type":"text","text":""}]}
	],"usage":{}}`
	_, p := newConvRecorder(t, "application/json", body)
	res := generateWith(t, p, &provider.GenerateOptions{Prompt: whoWon(), Tools: []types.Tool{searchTool("search")}})
	if len(res.Content) != 3 {
		t.Fatalf("content = %#v", res.Content)
	}
	if res.Content[0].(types.TextContent).Text != "Hello" || res.Content[1].(types.ReasoningContent).Text != "Think" {
		t.Errorf("content = %#v", res.Content)
	}
	if src := res.Content[2].(types.SourceContent); src.URL != "https://example.com/results" || src.Title != "Results" {
		t.Errorf("source = %#v", src)
	}
}

func TestConversationReferencesWithMissingFields(t *testing.T) {
	const url = "https://example.com/results"
	cases := []struct{ name, chunk, title string }{
		{"missing title", `{"type":"tool_reference","tool":"web_search","url":"` + url + `"}`, url},
		{"missing tool", `{"type":"tool_reference","title":"Results","url":"` + url + `"}`, "Results"},
		{"missing title and tool", `{"type":"tool_reference","url":"` + url + `"}`, url},
		{"null title and tool", `{"type":"tool_reference","title":null,"tool":null,"url":"` + url + `"}`, url},
	}
	for _, tc := range cases {
		t.Run("generate/"+tc.name, func(t *testing.T) {
			_, p := newConvRecorder(t, "application/json", `{"conversation_id":"conversation-1","outputs":[{"type":"message.output","content":[`+tc.chunk+`,{"type":"text","text":"Answer"}]}],"usage":`+convUsage+`}`)
			res := generateWith(t, p, &provider.GenerateOptions{Prompt: whoWon(), Tools: []types.Tool{searchTool("search")}})
			var haveText, haveSource bool
			for _, c := range res.Content {
				switch v := c.(type) {
				case types.TextContent:
					haveText = v.Text == "Answer"
				case types.SourceContent:
					haveSource = v.URL == url && v.Title == tc.title
				}
			}
			if !haveText || !haveSource || res.FinishReason != types.FinishReasonStop {
				t.Errorf("content = %#v finish = %v", res.Content, res.FinishReason)
			}
		})
		t.Run("stream/"+tc.name, func(t *testing.T) {
			_, p := newConvRecorder(t, "text/event-stream", sse(
				`{"type":"message.output.delta","id":"message-1","content":`+tc.chunk+`}`,
				`{"type":"message.output.delta","id":"message-1","content":"Answer"}`,
				`{"type":"conversation.response.done","usage":`+convUsage+`}`,
			))
			chunks := streamWith(t, p, &provider.GenerateOptions{Prompt: whoWon(), Tools: []types.Tool{searchTool("search")}})
			var haveText, haveSource bool
			for _, c := range chunks {
				switch c.Type {
				case provider.ChunkTypeText:
					haveText = c.ID == "message-1-0" && c.Text == "Answer"
				case provider.ChunkTypeSource:
					haveSource = c.SourceContent.URL == url && c.SourceContent.Title == tc.title
				case provider.ChunkTypeError:
					t.Errorf("unexpected error chunk %#v", c)
				}
			}
			if last := chunks[len(chunks)-1]; !haveText || !haveSource || last.FinishReason != types.FinishReasonStop {
				t.Errorf("chunks = %v", chunkTypes(chunks))
			}
		})
	}
}

func TestConversationWarnsAboutUnsupportedOptions(t *testing.T) {
	rec, p := newConvRecorder(t, "application/json", convOutputs(convExecution, convAnswer))
	res := generateWith(t, p, &provider.GenerateOptions{
		Prompt: whoWon(), Tools: []types.Tool{searchTool("search")},
		ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{
			"safePrompt": true, "parallelToolCalls": false, "promptCacheKey": "cache", "documentImageLimit": 1.0, "documentPageLimit": 2.0,
		}},
	})
	var features []string
	for _, w := range res.Warnings {
		features = append(features, w.Feature)
	}
	want := []string{"safePrompt", "documentImageLimit", "documentPageLimit", "promptCacheKey", "parallelToolCalls"}
	if !reflect.DeepEqual(features, want) {
		t.Errorf("warnings = %v, want %v", features, want)
	}
	body := rec.body(0)
	if _, ok := body["safe_prompt"]; ok {
		t.Errorf("safe_prompt sent")
	}
	if _, ok := body["completion_args"].(map[string]interface{})["parallel_tool_calls"]; ok {
		t.Errorf("parallel_tool_calls sent")
	}
}

func TestConversationStreamSearchReasoningTextReferencesAndUsage(t *testing.T) {
	rec, p := newConvRecorder(t, "text/event-stream", sse(
		`{"type":"conversation.response.started","conversation_id":"conversation-1","created_at":"2026-01-01T00:00:00Z"}`,
		`{"type":"tool.execution.started","id":"search-1","name":"web_search","arguments":""}`,
		`{"type":"tool.execution.delta","id":"search-1","name":"web_search","arguments":"latest "}`,
		`{"type":"tool.execution.delta","id":"search-1","name":"web_search","arguments":"results"}`,
		`{"type":"tool.execution.done","id":"search-1","name":"web_search","info":{"status":"success"}}`,
		`{"type":"message.output.delta","id":"message-1","content":{"type":"thinking","thinking":[{"type":"text","text":"Think"}]}}`,
		`{"type":"message.output.delta","id":"message-1","content_index":1,"content":"Spain "}`,
		`{"type":"message.output.delta","id":"message-1","content_index":1,"content":{"type":"text","text":"won."}}`,
		`{"type":"message.output.delta","id":"message-1","content_index":2,"content":`+convReference+`}`,
		`{"type":"conversation.response.done","usage":`+convUsage+`}`,
	))
	tools := []types.Tool{searchTool("search")}
	chunks := streamWith(t, p, &provider.GenerateOptions{Prompt: whoWon(), Tools: tools})
	want := []provider.ChunkType{
		provider.ChunkTypeStreamStart, provider.ChunkTypeResponseMetadata,
		provider.ChunkTypeToolInputStart, provider.ChunkTypeToolInputDelta, provider.ChunkTypeToolInputEnd,
		provider.ChunkTypeToolCall, provider.ChunkTypeToolResult,
		provider.ChunkTypeReasoningStart, provider.ChunkTypeReasoning, provider.ChunkTypeReasoningEnd,
		provider.ChunkTypeTextStart, provider.ChunkTypeText, provider.ChunkTypeText,
		provider.ChunkTypeSource, provider.ChunkTypeTextEnd, provider.ChunkTypeFinish,
	}
	if got := chunkTypes(chunks); !reflect.DeepEqual(got, want) {
		t.Fatalf("chunk types = %v\nwant %v", got, want)
	}
	call := findChunk(chunks, provider.ChunkTypeToolCall).ToolCall
	if call.ToolName != "search" || !call.ProviderExecuted || call.RawArguments != "" && call.RawArguments != `{"arguments":"latest results"}` {
		t.Errorf("call = %#v", call)
	}
	if call.Arguments["arguments"] != "latest results" {
		t.Errorf("call arguments = %#v", call.Arguments)
	}
	if id := findChunk(chunks, provider.ChunkTypeReasoning).ID; id != "message-1-0" {
		t.Errorf("reasoning id = %s", id)
	}
	if id := findChunk(chunks, provider.ChunkTypeText).ID; id != "message-1-1" {
		t.Errorf("text id = %s", id)
	}
	last := chunks[len(chunks)-1]
	if last.FinishReason != types.FinishReasonStop || *last.Usage.InputTokens != 10 || *last.Usage.OutputTokens != 20 {
		t.Errorf("finish = %#v", last)
	}
	if rec.body(0)["stream"] != true || !reflect.DeepEqual(rec.body(0)["tools"], []interface{}{map[string]interface{}{"type": "web_search"}}) {
		t.Errorf("body = %#v", rec.body(0))
	}

	// The streamed tool call replays on a follow-up turn.
	rec.set("application/json", `{"conversation_id":"conversation-2","outputs":[`+convAnswer+`],"usage":`+convUsage+`}`)
	input, _ := json.Marshal(call.Arguments)
	generateWith(t, p, &provider.GenerateOptions{Prompt: types.Prompt{Messages: followupMessages(types.ToolCallContent{ToolCallID: call.ID, ToolName: call.ToolName, Input: string(input)}, map[string]interface{}{"status": "success"})}, Tools: tools})
	if in := rec.body(1)["inputs"].([]interface{})[1].(map[string]interface{}); in["type"] != "function.call" || in["arguments"] != "latest results" {
		t.Errorf("replayed call = %#v", in)
	}
}

func TestConversationStreamIncrementalFunctionCallsAlongsideSearch(t *testing.T) {
	_, p := newConvRecorder(t, "text/event-stream", sse(
		`{"type":"function.call.delta","tool_call_id":"function-1","name":"weather","arguments":"{\"city\":","output_index":0}`,
		`{"type":"function.call.delta","tool_call_id":"function-1","name":"weather","arguments":"\"Madrid\"}","output_index":0}`,
		`{"type":"conversation.response.done","usage":`+convUsage+`}`,
	))
	chunks := streamWith(t, p, &provider.GenerateOptions{Prompt: whoWon(), Tools: []types.Tool{searchTool("search")}})
	call := findChunk(chunks, provider.ChunkTypeToolCall)
	if call == nil || call.ToolCall.ID != "function-1" || call.ToolCall.ToolName != "weather" || call.ToolCall.Arguments["city"] != "Madrid" {
		t.Fatalf("call = %#v", call)
	}
	if last := chunks[len(chunks)-1]; last.FinishReason != types.FinishReasonToolCalls {
		t.Errorf("finish = %v", last.FinishReason)
	}
}

func TestConversationStreamPremiumSearchWithoutInfoAndModelID(t *testing.T) {
	_, p := newConvRecorder(t, "text/event-stream", sse(
		`{"type":"tool.execution.started","id":"search-1","name":"web_search_premium","arguments":"query","function":"web_search"}`,
		`{"type":"tool.execution.done","id":"search-1","name":"web_search_premium"}`,
		`{"type":"message.output.delta","id":"message-1","model":"mistral-small-latest","content":"Answer"}`,
		`{"type":"conversation.response.done","usage":`+convUsage+`}`,
	))
	chunks := streamWith(t, p, &provider.GenerateOptions{Prompt: whoWon(), Tools: []types.Tool{premiumTool("news")}})
	call := findChunk(chunks, provider.ChunkTypeToolCall).ToolCall
	if mistral, _ := call.ProviderMetadata["mistral"].(map[string]interface{}); mistral["function"] != "web_search" {
		t.Errorf("metadata = %#v", call.ProviderMetadata)
	}
	res := findChunk(chunks, provider.ChunkTypeToolResult).ToolResult
	if res.ToolCallID != "search-1" || res.ToolName != "news" || !reflect.DeepEqual(res.Result, map[string]interface{}{}) {
		t.Errorf("result = %#v", res)
	}
	var modelMeta bool
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeResponseMetadata && c.ResponseMetadata.ModelID == "mistral-small-latest" {
			modelMeta = true
		}
	}
	if !modelMeta {
		t.Errorf("no model id metadata in %v", chunkTypes(chunks))
	}
	if last := chunks[len(chunks)-1]; last.FinishReason != types.FinishReasonStop {
		t.Errorf("finish = %v", last.FinishReason)
	}
}

func TestConversationStreamReportsFailedOrIncompleteStreams(t *testing.T) {
	cases := map[string][]string{
		"error event":         {`{"type":"conversation.response.error","message":"Search failed","code":500}`},
		"invalid content":     {`{"type":"message.output.delta","id":"message-1","content":42}`, `{"type":"conversation.response.done","usage":` + convUsage + `}`},
		"unfinished tool":     {`{"type":"tool.execution.started","id":"search-1","name":"web_search","arguments":""}`, `{"type":"conversation.response.done","usage":` + convUsage + `}`},
		"truncated":           {`{"type":"message.output.delta","id":"message-1","content":"Truncated"}`},
		"unknown event":       {`{"type":"agent.handoff.started"}`, `{"type":"conversation.response.done","usage":` + convUsage + `}`},
		"done without start":  {`{"type":"tool.execution.done","id":"nope","name":"web_search"}`, `{"type":"conversation.response.done","usage":` + convUsage + `}`},
		"invalid json event":  {`{not json`, `{"type":"conversation.response.done","usage":` + convUsage + `}`},
		"no events at all":    {},
		"text then no finish": {`{"type":"message.output.delta","id":"m","content":"x"}`},
	}
	for name, events := range cases {
		t.Run(name, func(t *testing.T) {
			_, p := newConvRecorder(t, "text/event-stream", sse(events...))
			chunks := streamWith(t, p, &provider.GenerateOptions{Prompt: whoWon(), Tools: []types.Tool{searchTool("search")}})
			last := chunks[len(chunks)-1]
			if last.Type != provider.ChunkTypeFinish || last.FinishReason != types.FinishReasonError {
				t.Errorf("last = %#v (types %v)", last, chunkTypes(chunks))
			}
		})
	}
}

func TestConversationStreamIncludesRawEvents(t *testing.T) {
	_, p := newConvRecorder(t, "text/event-stream", sse(`{"type":"conversation.response.done","usage":`+convUsage+`}`))
	chunks := streamWith(t, p, &provider.GenerateOptions{Prompt: whoWon(), Tools: []types.Tool{searchTool("search")}, IncludeRawChunks: true})
	raw := findChunk(chunks, provider.ChunkTypeRaw)
	m, _ := raw.Raw.(map[string]interface{})
	if raw == nil || m["type"] != "conversation.response.done" {
		t.Errorf("raw = %#v", raw)
	}
}

func premiumNames() conversationToolNames {
	return newConversationToolNames([]types.Tool{premiumTool("news")})
}

func TestConversationInputsOmitEntryIDsAndKeepFunctionCallIDs(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "tool_exec_search", ToolName: "news", ProviderExecuted: true, Input: `{"arguments":"{\"query\":\"news\"}"}`,
				ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{"function": "web_search"}}},
			types.ToolResultContent{ToolCallID: "tool_exec_search", ToolName: "news", Output: jsonOut(map[string]interface{}{"info": map[string]interface{}{"result": "search context"}})},
			types.ToolCallContent{ToolCallID: "function-1", ToolName: "weather", Input: `{"city":"Paris"}`},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "function-1", ToolName: "weather", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "Sunny"}},
		}},
	}
	inputs, _, err := convertToMistralConversationInputs(messages, premiumNames())
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range inputs {
		if _, ok := in["id"]; ok {
			t.Errorf("input has id: %#v", in)
		}
	}
	want := []map[string]interface{}{
		{"type": "function.call", "tool_call_id": "tool_exec_search", "name": "web_search", "arguments": `{"query":"news"}`},
		{"type": "function.result", "tool_call_id": "tool_exec_search", "result": "search context"},
		{"type": "function.call", "tool_call_id": "function-1", "name": "weather", "arguments": `{"city":"Paris"}`},
		{"type": "function.result", "tool_call_id": "function-1", "result": "Sunny"},
	}
	if !reflect.DeepEqual(inputs, want) {
		t.Errorf("inputs = %#v", inputs)
	}
}

func TestConversationInputsPreserveOrderReasoningAndFunctionResults(t *testing.T) {
	messages := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.TextContent{Text: "Before"}, types.ReasoningContent{Text: "Think"},
		types.ToolCallContent{ToolCallID: "function-1", ToolName: "weather", Input: `{"city":"Paris"}`},
		types.ToolResultContent{ToolCallID: "function-1", ToolName: "weather", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "Sunny"}},
		types.TextContent{Text: "After"},
	}}}
	inputs, _, err := convertToMistralConversationInputs(messages, premiumNames())
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]interface{}{
		{"type": "message.input", "role": "assistant", "content": []map[string]interface{}{
			{"type": "text", "text": "Before"},
			{"type": "thinking", "thinking": []map[string]interface{}{{"type": "text", "text": "Think"}}, "closed": true},
		}},
		{"type": "function.call", "tool_call_id": "function-1", "name": "weather", "arguments": `{"city":"Paris"}`},
		{"type": "function.result", "tool_call_id": "function-1", "result": "Sunny"},
		{"type": "message.input", "role": "assistant", "content": []map[string]interface{}{{"type": "text", "text": "After"}}},
	}
	if !reflect.DeepEqual(inputs, want) {
		t.Errorf("inputs = %#v", inputs)
	}
}

func TestConversationInputsReplayRenamedPremiumSearch(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "search-1", ToolName: "news", ProviderExecuted: true, Input: `{"arguments":"news query"}`}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "search-1", ToolName: "news", Output: jsonOut(map[string]interface{}{"info": map[string]interface{}{"documents": []interface{}{"result"}}})}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "The news"}}},
	}
	inputs, _, err := convertToMistralConversationInputs(messages, premiumNames())
	if err != nil {
		t.Fatal(err)
	}
	want := []map[string]interface{}{
		{"type": "function.call", "tool_call_id": "search-1", "name": "web_search_premium", "arguments": "news query"},
		{"type": "function.result", "tool_call_id": "search-1", "result": `{"documents":["result"]}`},
		{"type": "message.input", "role": "assistant", "content": []map[string]interface{}{{"type": "text", "text": "The news"}}},
	}
	if !reflect.DeepEqual(inputs, want) {
		t.Errorf("inputs = %#v", inputs)
	}
}

func TestConversationInputsKeepImagesAndPDFsInUserMessages(t *testing.T) {
	messages := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.TextContent{Text: "Search for context"},
		types.ImageContent{URL: "https://example.com/image.png", MimeType: "image/png"},
		types.FileContent{URL: "https://example.com/document.pdf", MimeType: "application/pdf"},
	}}}
	inputs, _, err := convertToMistralConversationInputs(messages, premiumNames())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(inputs)
	want := `[{"content":[{"text":"Search for context","type":"text"},{"image_url":"https://example.com/image.png","type":"image_url"},{"document_url":"https://example.com/document.pdf","type":"document_url"}],"role":"user","type":"message.input"}]`
	if string(b) != want {
		t.Errorf("inputs = %s\nwant %s", b, want)
	}
}

func TestConversationInputsRejectUnsupportedAssistantContent(t *testing.T) {
	messages := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.FileContent{Data: []byte{1}, MimeType: "image/png"},
	}}}
	if _, _, err := convertToMistralConversationInputs(messages, premiumNames()); err == nil {
		t.Fatal("expected an error for unsupported assistant content")
	}
}

func TestConversationPrepareToolsMapsSearchToolsAndWarnsOnUnknownProviderTools(t *testing.T) {
	tools, choice, warnings := prepareConversationTools([]types.Tool{
		searchTool("search"), premiumTool("news"),
		{Name: "unknown", ProviderID: "mistral.unknown", Type: types.ToolTypeProviderDefined, ProviderExecuted: true},
	}, types.ToolChoice{})
	wantTools := []map[string]interface{}{{"type": "web_search"}, {"type": "web_search_premium"}}
	if !reflect.DeepEqual(tools, wantTools) || choice != nil {
		t.Errorf("tools = %v choice = %v", tools, choice)
	}
	if len(warnings) != 1 || warnings[0].Type != "unsupported" || warnings[0].Feature != "provider-defined tool mistral.unknown" {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestConversationPrepareToolsForcesFunctionToolWhenSearchAvailable(t *testing.T) {
	weather := types.Tool{Name: "weather", Parameters: map[string]interface{}{"type": "object"}}
	tools, choice, warnings := prepareConversationTools([]types.Tool{searchTool("search"), weather},
		types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "weather"})
	if choice != "any" || len(warnings) != 0 || len(tools) != 1 {
		t.Fatalf("tools = %v choice = %v warnings = %v", tools, choice, warnings)
	}
	fn := tools[0]["function"].(map[string]interface{})
	if tools[0]["type"] != "function" || fn["name"] != "weather" {
		t.Errorf("tools = %v", tools)
	}
}

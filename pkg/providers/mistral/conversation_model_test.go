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
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from TS #22237 (mistral-conversation-language-model.test.ts and
// convert-to-mistral-conversation-inputs.test.ts, core behaviors).

func conversationServer(t *testing.T, contentType, body string, captured *map[string]interface{}) *Provider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/conversations" {
			t.Errorf("path = %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, captured)
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(Config{APIKey: "k", BaseURL: srv.URL})
}

func userPrompt(text string) types.Prompt {
	return types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: text}}}}}
}

func TestConversationRequestBodyAndWarnings(t *testing.T) {
	var body map[string]interface{}
	p := conversationServer(t, "application/json", `{"conversation_id":"conv_1","outputs":[{"type":"message.output","content":"Hi","model":"mistral-medium-latest"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`, &body)
	temp := 0.5
	high := types.ReasoningHigh
	topK := 3
	result, err := p.ConversationModel("mistral-medium-latest").DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{System: "Be brief", Messages: userPrompt("Hello").Messages}, Temperature: &temp, Reasoning: &high, TopK: &topK,
		Tools:      []types.Tool{WebSearch()},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
		ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{
			"safePrompt": true, "promptCacheKey": "k", "documentPageLimit": 3.0,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if body["store"] != false || body["model"] != "mistral-medium-latest" || body["instructions"] != "Be brief" {
		t.Errorf("body = %#v", body)
	}
	if !reflect.DeepEqual(body["tools"], []interface{}{map[string]interface{}{"type": "web_search"}}) {
		t.Errorf("tools = %#v", body["tools"])
	}
	ca := body["completion_args"].(map[string]interface{})
	if ca["temperature"] != 0.5 || ca["reasoning_effort"] != "high" || ca["tool_choice"] != "any" {
		t.Errorf("completion_args = %#v", ca)
	}
	inputs := body["inputs"].([]interface{})
	first := inputs[0].(map[string]interface{})
	if first["type"] != "message.input" || first["role"] != "user" {
		t.Errorf("inputs = %#v", inputs)
	}
	var features []string
	for _, w := range result.Warnings {
		features = append(features, w.Feature)
	}
	want := []string{"topK", "safePrompt", "documentPageLimit", "promptCacheKey"}
	if !reflect.DeepEqual(features, want) {
		t.Errorf("warnings = %v, want %v", features, want)
	}
	if result.Text != "Hi" || result.FinishReason != types.FinishReasonStop || result.ResponseMetadata.ID != "conv_1" || result.ResponseMetadata.ModelID != "mistral-medium-latest" {
		t.Errorf("result = %#v", result)
	}
}

func TestConversationGenerateContentToolExecutionAndSources(t *testing.T) {
	var body map[string]interface{}
	p := conversationServer(t, "application/json", `{"conversation_id":"c","outputs":[
		{"type":"tool.execution","id":"te_1","name":"web_search","arguments":"{\"query\":\"x\"}","function":"search","info":{"k":"v"}},
		{"type":"message.output","content":[{"type":"thinking","thinking":[{"type":"text","text":"hmm"}]},{"type":"text","text":"Answer"},{"type":"tool_reference","tool":"web_search","title":"Doc","url":"https://a.test"},{"type":"tool_reference","url":"https://a.test"}]},
		{"type":"function.call","tool_call_id":"fc_1","name":"lookup","arguments":{"a":1}}
	]}`, &body)
	result, err := p.ConversationModel("mistral-medium-latest").DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: userPrompt("hi"), Tools: []types.Tool{WebSearch()},
	})
	if err != nil {
		t.Fatal(err)
	}
	call := result.Content[0].(types.ToolCallContent)
	if call.ToolCallID != "te_1" || call.ToolName != ToolIDWebSearch || !call.ProviderExecuted || call.Input != `{"arguments":"{\"query\":\"x\"}"}` {
		t.Errorf("call = %#v", call)
	}
	if !strings.Contains(string(call.ProviderMetadata), `"function":"search"`) {
		t.Errorf("metadata = %s", call.ProviderMetadata)
	}
	res := result.Content[1].(types.ToolResultContent)
	if !reflect.DeepEqual(res.Result, map[string]interface{}{"info": map[string]interface{}{"k": "v"}}) {
		t.Errorf("result = %#v", res.Result)
	}
	if r, ok := result.Content[2].(types.ReasoningContent); !ok || r.Text != "hmm" {
		t.Errorf("content[2] = %#v", result.Content[2])
	}
	if tc, ok := result.Content[3].(types.TextContent); !ok || tc.Text != "Answer" {
		t.Errorf("content[3] = %#v", result.Content[3])
	}
	src := result.Content[4].(types.SourceContent)
	if src.URL != "https://a.test" || src.Title != "Doc" || src.SourceType != "url" {
		t.Errorf("source = %#v", src)
	}
	// duplicate source URL is skipped; function call follows.
	fc := result.Content[5].(types.ToolCallContent)
	if fc.ToolName != "lookup" || fc.Input != `{"a":1}` || fc.ProviderExecuted {
		t.Errorf("function call = %#v", fc)
	}
	if result.FinishReason != types.FinishReasonToolCalls {
		t.Errorf("finish = %v", result.FinishReason)
	}
}

func drainConversation(t *testing.T, stream provider.TextStream) []*provider.StreamChunk {
	t.Helper()
	var chunks []*provider.StreamChunk
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		chunks = append(chunks, c)
	}
	return chunks
}

func sse(events ...string) string {
	var b strings.Builder
	for _, e := range events {
		fmt.Fprintf(&b, "data: %s\n\n", e)
	}
	return b.String()
}

func TestConversationStream(t *testing.T) {
	var body map[string]interface{}
	p := conversationServer(t, "text/event-stream", sse(
		`{"type":"conversation.response.started","conversation_id":"conv_9","created_at":"2026-10-14T00:00:00Z"}`,
		`{"type":"tool.execution.started","id":"te_1","name":"web_search","arguments":"{\"q\":"}`,
		`{"type":"tool.execution.delta","id":"te_1","name":"web_search","arguments":"1}"}`,
		`{"type":"tool.execution.done","id":"te_1","name":"web_search","info":{"r":1}}`,
		`{"type":"message.output.delta","id":"m1","content_index":0,"content":"Hel","model":"mistral-medium-latest"}`,
		`{"type":"message.output.delta","id":"m1","content_index":0,"content":{"type":"text","text":"lo"}}`,
		`{"type":"message.output.delta","id":"m1","content_index":1,"content":{"type":"tool_reference","title":"T","url":"https://a.test"}}`,
		`{"type":"conversation.response.done","usage":{"prompt_tokens":4,"completion_tokens":5,"total_tokens":9}}`,
	), &body)
	stream, err := p.ConversationModel("mistral-medium-latest").DoStream(context.Background(), &provider.GenerateOptions{Prompt: userPrompt("hi"), Tools: []types.Tool{WebSearch()}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	chunks := drainConversation(t, stream)
	if body["stream"] != true {
		t.Errorf("stream flag missing: %v", body)
	}
	var types_ []provider.ChunkType
	for _, c := range chunks {
		types_ = append(types_, c.Type)
	}
	want := []provider.ChunkType{
		provider.ChunkTypeStreamStart, provider.ChunkTypeResponseMetadata,
		provider.ChunkTypeToolInputStart, provider.ChunkTypeToolInputDelta, provider.ChunkTypeToolInputEnd,
		provider.ChunkTypeToolCall, provider.ChunkTypeToolResult,
		provider.ChunkTypeResponseMetadata,
		provider.ChunkTypeTextStart, provider.ChunkTypeText, provider.ChunkTypeText,
		provider.ChunkTypeSource,
		provider.ChunkTypeTextEnd, // closed by finish
		provider.ChunkTypeFinish,
	}
	if !reflect.DeepEqual(types_, want) {
		t.Fatalf("chunk types = %v\nwant %v", types_, want)
	}
	if chunks[3].Text != `{"arguments":"{\"q\":1}"}` {
		t.Errorf("tool input delta = %q", chunks[3].Text)
	}
	if chunks[5].ToolCall.ToolName != ToolIDWebSearch || !chunks[5].ToolCall.ProviderExecuted {
		t.Errorf("tool call = %#v", chunks[5].ToolCall)
	}
	last := chunks[len(chunks)-1]
	if last.FinishReason != types.FinishReasonStop || last.Usage == nil || *last.Usage.TotalTokens != 9 {
		t.Errorf("finish = %#v", last)
	}
}

func TestConversationStreamIncompleteIsError(t *testing.T) {
	var body map[string]interface{}
	p := conversationServer(t, "text/event-stream", sse(
		`{"type":"conversation.response.started","conversation_id":"c"}`,
		`{"type":"tool.execution.started","id":"te_1","name":"web_search","arguments":""}`,
	), &body)
	stream, err := p.ConversationModel("mistral-medium-latest").DoStream(context.Background(), &provider.GenerateOptions{Prompt: userPrompt("hi")})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	chunks := drainConversation(t, stream)
	if last := chunks[len(chunks)-1]; last.Type != provider.ChunkTypeFinish || last.FinishReason != types.FinishReasonError {
		t.Errorf("last = %#v", last)
	}
}

func TestConversationInputsReplayExecutionsAndFunctionCalls(t *testing.T) {
	names := newConversationToolNames([]types.Tool{WebSearch()})
	messages := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "S1"}}},
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "S2"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "q"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ReasoningContent{Text: "think"},
			types.ToolCallContent{ToolCallID: "te_1", ToolName: "web_search", Input: `{"arguments":"{\"q\":1}"}`, ProviderExecuted: true,
				ProviderOptions: map[string]interface{}{"mistral": map[string]interface{}{"function": "search"}}},
			types.ToolResultContent{ToolCallID: "te_1", ToolName: "web_search", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"info": map[string]interface{}{"result": "found"}}}},
			types.TextContent{Text: "answer"},
			types.ToolCallContent{ToolCallID: "fc_1", ToolName: "lookup", Input: `{"a":1}`},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "fc_1", ToolName: "lookup", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "done"}},
		}},
	}
	inputs, instructions, err := convertToMistralConversationInputs(messages, names)
	if err != nil {
		t.Fatal(err)
	}
	if instructions != "S1\n\nS2" {
		t.Errorf("instructions = %q", instructions)
	}
	var kinds []string
	for _, in := range inputs {
		kinds = append(kinds, in["type"].(string))
	}
	want := []string{"message.input", "message.input", "function.call", "function.result", "message.input", "function.call", "function.result"}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds = %v", kinds)
	}
	if inputs[2]["name"] != "search" || inputs[2]["arguments"] != `{"q":1}` || inputs[3]["result"] != "found" {
		t.Errorf("execution replay = %#v %#v", inputs[2], inputs[3])
	}
	if inputs[5]["name"] != "lookup" || inputs[6]["result"] != "done" {
		t.Errorf("function replay = %#v %#v", inputs[5], inputs[6])
	}
}

func TestConversationPrepareToolsChoiceTool(t *testing.T) {
	fn := types.Tool{Name: "lookup", Description: "d", Parameters: map[string]interface{}{"type": "object"}}
	tools, choice, warnings := prepareConversationTools([]types.Tool{WebSearch(), WebSearchPremium(), fn},
		types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "web_search"})
	if choice != "any" || len(warnings) != 0 || !reflect.DeepEqual(tools, []map[string]interface{}{{"type": "web_search"}}) {
		t.Errorf("tools=%v choice=%v warnings=%v", tools, choice, warnings)
	}
	custom := WebSearch()
	custom.Name = "search"
	tools, _, _ = prepareConversationTools([]types.Tool{custom, fn}, types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "search"})
	if !reflect.DeepEqual(tools, []map[string]interface{}{{"type": "web_search"}}) {
		t.Errorf("custom-named tools = %v", tools)
	}
	_, _, warnings = prepareConversationTools([]types.Tool{{Name: "x", ProviderID: "other.tool", ProviderExecuted: true}}, types.ToolChoice{})
	if len(warnings) != 1 || warnings[0].Feature != "provider-defined tool other.tool" {
		t.Errorf("warnings = %v", warnings)
	}
}

func TestConversationReasoningWarnsOnUnsupportedModel(t *testing.T) {
	var body map[string]interface{}
	p := conversationServer(t, "application/json", `{"conversation_id":"c","outputs":[]}`, &body)
	high := types.ReasoningHigh
	result, err := p.ConversationModel("codestral-latest").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: userPrompt("x"), Reasoning: &high})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Feature != "reasoning" {
		t.Errorf("warnings = %v", result.Warnings)
	}
	if _, ok := body["completion_args"].(map[string]interface{})["reasoning_effort"]; ok {
		t.Errorf("reasoning_effort sent: %v", body)
	}
}

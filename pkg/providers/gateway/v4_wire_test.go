package gateway

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
)

// The gateway answers LanguageModelV4 requests with V4 payloads: a content
// array, {unified, raw} finish reasons and nested usage.
func TestDoGenerateDecodesV4GenerateResult(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "content":[
		    {"type":"reasoning","text":"hmm","providerMetadata":{"x":{"a":1}}},
		    {"type":"text","text":"Hello","providerMetadata":{"gateway":{"b":2}}},
		    {"type":"tool-call","toolCallId":"c1","toolName":"weather","input":"{\"city\":\"NYC\"}"},
		    {"type":"source","sourceType":"url","id":"s1","url":"https://a.test","title":"A"}
		  ],
		  "finishReason":{"unified":"tool-calls","raw":"tool_use"},
		  "usage":{"inputTokens":{"total":10,"noCache":8,"cacheRead":2},"outputTokens":{"total":5,"text":3,"reasoning":2},"raw":{"x":1}},
		  "warnings":[{"type":"other","message":"w"}],
		  "providerMetadata":{"gateway":{"routing":"r"}},
		  "response":{"id":"resp_1","modelId":"openai/gpt-5","timestamp":"2026-06-19T17:59:31Z"}
		}`))
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.LanguageModel("openai/gpt-5")
	if err != nil {
		t.Fatal(err)
	}
	maxTokens := 7
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{System: "be brief", Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ReasoningContent{Text: "think"},
				types.ToolCallContent{ToolCallID: "c0", ToolName: "weather", Input: `{"city":"LA"}`},
			}},
			{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
				ToolCallID: "c0", ToolName: "weather", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"t": 1}},
			}}},
		}},
		MaxTokens:  &maxTokens,
		Tools:      []types.Tool{{Name: "weather", Parameters: map[string]interface{}{"type": "object"}}},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceAuto},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Request: V4 call options.
	if gotBody["maxOutputTokens"] != float64(7) || gotBody["maxTokens"] != nil || gotBody["system"] != nil {
		t.Fatalf("request options not V4: %v", gotBody)
	}
	prompt := gotBody["prompt"].([]interface{})
	if prompt[0].(map[string]interface{})["role"] != "system" || prompt[0].(map[string]interface{})["content"] != "be brief" {
		t.Fatalf("system message = %v", prompt[0])
	}
	assistant := prompt[2].(map[string]interface{})["content"].([]interface{})
	if assistant[0].(map[string]interface{})["type"] != "reasoning" {
		t.Fatalf("assistant reasoning part missing: %v", assistant)
	}
	call := assistant[1].(map[string]interface{})
	if call["type"] != "tool-call" || call["input"].(map[string]interface{})["city"] != "LA" {
		t.Fatalf("assistant tool-call part = %v", call)
	}
	tool := gotBody["tools"].([]interface{})[0].(map[string]interface{})
	if tool["type"] != "function" || tool["inputSchema"] == nil || tool["parameters"] != nil {
		t.Fatalf("tool = %v", tool)
	}

	// Response: V4 decoded.
	if res.FinishReason != types.FinishReasonToolCalls || res.RawFinishReason != "tool_use" {
		t.Fatalf("finish = %q/%q", res.FinishReason, res.RawFinishReason)
	}
	if res.Text != "Hello" || len(res.Content) != 4 || len(res.ToolCalls) != 1 || res.ToolCalls[0].Arguments["city"] != "NYC" {
		t.Fatalf("content = %#v text=%q calls=%#v", res.Content, res.Text, res.ToolCalls)
	}
	if res.Usage.InputTokens == nil || *res.Usage.InputTokens != 10 || *res.Usage.InputDetails.CacheReadTokens != 2 ||
		*res.Usage.OutputDetails.ReasoningTokens != 2 || res.Usage.Raw["x"] != float64(1) {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if len(res.Warnings) != 1 || res.ProviderMetadata["gateway"] == nil {
		t.Fatalf("warnings/metadata = %v / %v", res.Warnings, res.ProviderMetadata)
	}
	// TS overrides the body's `response` with {headers, body}: server id/model/timestamp are dropped.
	if res.ResponseMetadata == nil || res.ResponseMetadata.ID != "" || res.ResponseMetadata.ModelID != "" || !res.ResponseMetadata.Timestamp.IsZero() {
		t.Fatalf("response metadata = %+v", res.ResponseMetadata)
	}
}

func TestDoStreamDecodesV4Parts(t *testing.T) {
	stream := &gatewayTextStream{
		parser: streamingParser(strings.Join([]string{
			`data: {"type":"tool-input-start","id":"c1","toolName":"weather"}`, "",
			`data: {"type":"tool-input-delta","id":"c1","delta":"{\"city\""}`, "",
			`data: {"type":"tool-input-end","id":"c1"}`, "",
			`data: {"type":"tool-call","toolCallId":"c1","toolName":"weather","input":"{\"city\":\"NYC\"}"}`, "",
			`data: {"type":"tool-result","toolCallId":"c1","toolName":"weather","result":{"t":1},"providerExecuted":true}`, "",
			`data: {"type":"finish","finishReason":{"unified":"stop","raw":"end_turn"},"usage":{"inputTokens":{"total":4},"outputTokens":{"total":2}},"providerMetadata":{"gateway":{"k":1}}}`, "",
			`data: [DONE]`, "",
		}, "\n")),
		body: io.NopCloser(strings.NewReader("")),
	}
	var got []*provider.StreamChunk
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		got = append(got, c)
	}
	want := []provider.ChunkType{
		provider.ChunkTypeToolInputStart, provider.ChunkTypeToolInputDelta, provider.ChunkTypeToolInputEnd,
		provider.ChunkTypeToolCall, provider.ChunkTypeToolResult, provider.ChunkTypeFinish,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d chunks %v, want %d", len(got), got, len(want))
	}
	for i, w := range want {
		if got[i].Type != w {
			t.Fatalf("chunk %d = %s, want %s", i, got[i].Type, w)
		}
	}
	if got[0].ToolCall.ToolName != "weather" || got[1].Text != `{"city"` {
		t.Fatalf("tool input chunks = %+v / %+v", got[0].ToolCall, got[1])
	}
	if got[3].ToolCall.Arguments["city"] != "NYC" || got[3].ToolCall.RawArguments != `{"city":"NYC"}` {
		t.Fatalf("tool call = %+v", got[3].ToolCall)
	}
	fin := got[5]
	if fin.FinishReason != types.FinishReasonStop || fin.RawFinishReason != "end_turn" || fin.Usage == nil ||
		*fin.Usage.InputTokens != 4 || fin.ProviderMetadata == nil {
		t.Fatalf("finish = %+v", fin)
	}
}

func TestGatewayStreamToolResultKeepsPreliminaryAndMetadata(t *testing.T) {
	stream := &gatewayTextStream{
		parser: streamingParser(strings.Join([]string{
			`data: {"type":"tool-input-start","id":"c1","toolName":"w","providerMetadata":{"gateway":{"a":1}}}`, "",
			`data: {"type":"tool-result","toolCallId":"c1","toolName":"w","result":{"t":1},"preliminary":true,"providerMetadata":{"gateway":{"b":2}}}`, "",
			`data: [DONE]`, "",
		}, "\n")),
		body: io.NopCloser(strings.NewReader("")),
	}
	start, err := stream.Next()
	if err != nil || len(start.ProviderMetadata) == 0 {
		t.Fatalf("tool-input-start lost providerMetadata: %#v err=%v", start, err)
	}
	res, err := stream.Next()
	if err != nil || res.ToolResult == nil || !res.ToolResult.Preliminary || !res.ToolResult.ProviderExecuted || res.ToolResult.ProviderMetadata == nil {
		t.Fatalf("tool-result lost fields: %#v err=%v", res, err)
	}
}

func TestGatewayStreamToolCallKeepsExplicitProviderExecutedFalse(t *testing.T) {
	stream := &gatewayTextStream{
		parser: streamingParser(strings.Join([]string{
			`data: {"type":"tool-call","toolCallId":"a","toolName":"w","input":"{}","providerExecuted":false}`, "",
			`data: {"type":"tool-call","toolCallId":"b","toolName":"w","input":"{}"}`, "",
			`data: [DONE]`, "",
		}, "\n")),
		body: io.NopCloser(strings.NewReader("")),
	}
	a, err := stream.Next()
	if err != nil || a.ToolCall == nil || a.ToolCall.ProviderExecuted || !a.ToolCall.ProviderExecutedSet {
		t.Fatalf("explicit false lost: %#v err=%v", a, err)
	}
	b, err := stream.Next()
	if err != nil || b.ToolCall == nil || b.ToolCall.ProviderExecutedSet {
		t.Fatalf("omitted must stay unset: %#v err=%v", b, err)
	}
}

// tool-input-start keeps an explicit providerExecuted false too.
func TestGatewayStreamToolInputStartKeepsExplicitProviderExecutedFalse(t *testing.T) {
	stream := &gatewayTextStream{
		parser: streamingParser(strings.Join([]string{
			`data: {"type":"tool-input-start","id":"a","toolName":"w","providerExecuted":false}`, "",
			`data: {"type":"tool-input-start","id":"b","toolName":"w"}`, "",
			`data: [DONE]`, "",
		}, "\n")),
		body: io.NopCloser(strings.NewReader("")),
	}
	a, err := stream.Next()
	if err != nil || a.ToolCall == nil || a.ToolCall.ProviderExecuted || !a.ToolCall.ProviderExecutedSet {
		t.Fatalf("explicit false lost: %#v err=%v", a, err)
	}
	b, err := stream.Next()
	if err != nil || b.ToolCall == nil || b.ToolCall.ProviderExecutedSet {
		t.Fatalf("omitted must stay unset: %#v err=%v", b, err)
	}
}

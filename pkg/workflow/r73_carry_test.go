package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS WorkflowAgent.prepareInvocation hands prepareCall the constructor's
// generation settings (`{...this.generationSettings}`) and merges the call's
// own settings over the hook's result afterwards, so the hook sees the agent
// values while the model call uses the call's.
func TestR73Carry_PrepareCallSeesAgentGenerationSettings(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	i := func(v int) *int { return &v }
	for _, stream := range []bool{false, true} {
		name := "generate"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			model := &wfJSONModel{}
			var seen LanguageModelCallOptions
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: model, Temperature: f(0.1), MaxTokens: i(10), MaxRetries: i(1),
				Headers:         map[string]string{"x-agent": "a"},
				ProviderOptions: map[string]interface{}{"p": map[string]interface{}{"agent": true}},
				PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					seen = o
					return o, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			callHeaders := map[string]string{"x-call": "c"}
			callProvider := map[string]interface{}{"p": map[string]interface{}{"call": true}}
			if stream {
				res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{
					Prompt: "hi", Temperature: f(0.9), MaxTokens: i(99), MaxRetries: i(0), Headers: callHeaders, ProviderOptions: callProvider,
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := res.ConsumeStream(); err != nil {
					t.Fatal(err)
				}
			} else if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{
				Prompt: "hi", Temperature: f(0.9), MaxTokens: i(99), MaxRetries: i(0), Headers: callHeaders, ProviderOptions: callProvider,
			}); err != nil {
				t.Fatal(err)
			}
			if seen.Temperature == nil || *seen.Temperature != 0.1 || seen.MaxTokens == nil || *seen.MaxTokens != 10 {
				t.Errorf("hook settings = temperature %v maxTokens %v, want agent 0.1/10", seen.Temperature, seen.MaxTokens)
			}
			if seen.MaxRetries == nil || *seen.MaxRetries != 1 {
				t.Errorf("hook maxRetries = %v, want agent 1", seen.MaxRetries)
			}
			if seen.Headers["x-agent"] != "a" || seen.Headers["x-call"] != "" {
				t.Errorf("hook headers = %v, want the agent's", seen.Headers)
			}
			if _, ok := seen.ProviderOptions["p"].(map[string]interface{})["agent"]; !ok {
				t.Errorf("hook providerOptions = %v, want the agent's", seen.ProviderOptions)
			}
			all := append(append([]*provider.GenerateOptions{}, model.opts...), model.wfMockModel.opts...)
			if len(all) == 0 {
				t.Fatal("no model call")
			}
			got := all[0]
			if got.Temperature == nil || *got.Temperature != 0.9 || got.MaxTokens == nil || *got.MaxTokens != 99 {
				t.Errorf("model settings = temperature %v maxTokens %v, want the call's 0.9/99", got.Temperature, got.MaxTokens)
			}
			if got.Headers["x-call"] != "c" || got.Headers["x-agent"] != "" {
				t.Errorf("model headers = %v, want the call's", got.Headers)
			}
		})
	}
}

// r73 prova CR: TS serializable-schema resolve makes a `type: 'provider'` def
// provider-executed only when `isProviderExecuted` is set, so a
// client-executed provider tool (anthropic bash, computer, text editor,
// memory) keeps running its Execute after a workflow round trip.
func TestR73Carry_ResolveClientProviderToolNotProviderExecuted(t *testing.T) {
	defs, err := SerializeToolSet([]types.Tool{
		{Name: "bash", Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.bash_20250124",
			ProviderArgs: map[string]interface{}{}, Parameters: map[string]interface{}{"type": "object"}, SupportsDeferredResults: true},
		{Name: "web", Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.web_search_20250305",
			ProviderArgs: map[string]interface{}{}, Parameters: map[string]interface{}{"type": "object"}, ProviderExecuted: true, SupportsDeferredResults: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]types.Tool{}
	for _, tool := range ResolveSerializableTools(defs) {
		got[tool.Name] = tool
	}
	bash := got["bash"]
	if bash.ProviderExecuted || bash.SupportsDeferredResults {
		t.Errorf("bash: providerExecuted=%v supportsDeferredResults=%v, want false/false", bash.ProviderExecuted, bash.SupportsDeferredResults)
	}
	if bash.Type != types.ToolTypeProviderDefined || bash.ProviderID != "anthropic.bash_20250124" {
		t.Errorf("bash identity = %q %q", bash.Type, bash.ProviderID)
	}
	web := got["web"]
	if !web.ProviderExecuted || !web.SupportsDeferredResults || web.ProviderID != "anthropic.web_search_20250305" {
		t.Errorf("web: providerExecuted=%v supportsDeferredResults=%v id=%q", web.ProviderExecuted, web.SupportsDeferredResults, web.ProviderID)
	}
}

// r73carryMetaModel streams, on its first call, a text block with provider
// metadata and two tool calls (one with provider metadata, one without),
// then answers "done", recording every call's prompt.
type r73carryMetaModel struct {
	wfMockModel
	mu      sync.Mutex
	prompts [][]types.Message
}

func (m *r73carryMetaModel) DoStream(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	m.mu.Lock()
	m.prompts = append(m.prompts, append([]types.Message(nil), opts.Prompt.Messages...))
	n := len(m.prompts)
	m.mu.Unlock()
	if n == 1 {
		textMeta := json.RawMessage(`{"openai":{"itemId":"message-1","phase":"final_answer"}}`)
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "t1", ProviderMetadata: textMeta},
			{Type: provider.ChunkTypeText, ID: "t1", Text: "An answer."},
			{Type: provider.ChunkTypeTextEnd, ID: "t1", ProviderMetadata: textMeta},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "testTool", Arguments: map[string]interface{}{"value": "a"},
				ProviderMetadata: map[string]interface{}{"google": map[string]interface{}{"thoughtSignature": "sig-1"}}}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-2", ToolName: "testTool", Arguments: map[string]interface{}{"value": "b"}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}
	return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "done"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}), nil
}

func (m *r73carryMetaModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.mu.Lock()
	m.prompts = append(m.prompts, append([]types.Message(nil), opts.Prompt.Messages...))
	n := len(m.prompts)
	m.mu.Unlock()
	if n == 1 {
		return &types.GenerateResult{
			Content: []types.ContentPart{
				types.TextContent{Text: "An answer.", ProviderMetadata: json.RawMessage(`{"openai":{"itemId":"message-1","phase":"final_answer"}}`)},
			},
			Text: "An answer.",
			ToolCalls: []types.ToolCall{
				{ID: "call-1", ToolName: "testTool", Arguments: map[string]interface{}{"value": "a"}, ProviderMetadata: map[string]interface{}{"google": map[string]interface{}{"thoughtSignature": "sig-1"}}},
				{ID: "call-2", ToolName: "testTool", Arguments: map[string]interface{}{"value": "b"}},
			},
			FinishReason: types.FinishReasonToolCalls,
		}, nil
	}
	return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
}

// TS model-call-iterator.test.ts "providerMetadata to providerOptions
// mapping": the next step's assistant message carries the text part's and
// each tool call's provider metadata as providerOptions, and a tool call
// without metadata gets none (preserves text provider metadata, should
// preserve providerMetadata as providerOptions in tool-call messages, should
// handle mixed tool calls with and without providerMetadata, should not add
// providerOptions when providerMetadata is undefined).
func TestR73Carry_ProviderMetadataBecomesProviderOptions(t *testing.T) {
	for _, stream := range []bool{false, true} {
		model := &r73carryMetaModel{}
		a, err := NewWorkflowAgent(WorkflowAgent{
			Model: model,
			Tools: []types.Tool{{Name: "testTool", Parameters: map[string]interface{}{"type": "object"},
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) { return "ok", nil }}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if stream {
			st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "test"})
			if err != nil {
				t.Fatal(err)
			}
			if err := st.ConsumeStream(); err != nil {
				t.Fatal(err)
			}
		} else if _, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "test"}); err != nil {
			t.Fatal(err)
		}
		model.mu.Lock()
		prompts := model.prompts
		model.mu.Unlock()
		if len(prompts) != 2 {
			t.Fatalf("stream=%v: %d model calls, want 2", stream, len(prompts))
		}
		var assistant *types.Message
		for i := range prompts[1] {
			if prompts[1][i].Role == types.RoleAssistant {
				assistant = &prompts[1][i]
			}
		}
		if assistant == nil {
			t.Fatalf("stream=%v: no assistant message in %#v", stream, prompts[1])
		}
		var sawText bool
		calls := map[string]map[string]interface{}{}
		for _, part := range assistant.Content {
			switch p := part.(type) {
			case types.TextContent:
				sawText = true
				openai, _ := p.ProviderOptions["openai"].(map[string]interface{})
				if openai["itemId"] != "message-1" || openai["phase"] != "final_answer" {
					t.Errorf("stream=%v: text providerOptions = %#v", stream, p.ProviderOptions)
				}
			case types.ToolCallContent:
				calls[p.ToolCallID] = p.ProviderOptions
			}
		}
		if !sawText {
			t.Errorf("stream=%v: assistant message lost its text: %#v", stream, assistant.Content)
		}
		google, _ := calls["call-1"]["google"].(map[string]interface{})
		if google["thoughtSignature"] != "sig-1" {
			t.Errorf("stream=%v: call-1 providerOptions = %#v", stream, calls["call-1"])
		}
		if po, ok := calls["call-2"]; !ok || po != nil {
			t.Errorf("stream=%v: call-2 providerOptions = %#v (present=%v), want none", stream, po, ok)
		}
	}
}

// r73carryReasoningModel streams reasoning (with provider metadata), text and
// a tool call, then answers "done", recording every call's prompt.
type r73carryReasoningModel struct {
	r73carryMetaModel
}

func (m *r73carryReasoningModel) DoStream(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	m.mu.Lock()
	m.prompts = append(m.prompts, append([]types.Message(nil), opts.Prompt.Messages...))
	n := len(m.prompts)
	m.mu.Unlock()
	if n == 1 {
		meta := json.RawMessage(`{"anthropic":{"signature":"reasoning-signature"},"openai":{"reasoningEncryptedContent":"encrypted-reasoning"}}`)
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeReasoningStart, ID: "r1"},
			{Type: provider.ChunkTypeReasoning, ID: "r1", Reasoning: "Inspect the configuration first."},
			{Type: provider.ChunkTypeReasoningEnd, ID: "r1", ProviderMetadata: meta},
			{Type: provider.ChunkTypeTextStart, ID: "t1"},
			{Type: provider.ChunkTypeText, ID: "t1", Text: "I will inspect the configuration."},
			{Type: provider.ChunkTypeTextEnd, ID: "t1"},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "testTool", RawArguments: `{"query":"test"}`, Arguments: map[string]interface{}{"query": "test"},
				ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"itemId": "function-call-item", "reasoningEncryptedContent": "encrypted-reasoning"}}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}
	return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "done"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}), nil
}

// TS model-call-iterator.test.ts "replays reasoning and provider metadata
// with tool calls in emission order": the next step's assistant message is
// reasoning (its metadata as providerOptions), text, then the tool call (its
// metadata as providerOptions).
func TestR73Carry_ReasoningTextToolCallReplayOrder(t *testing.T) {
	model := &r73carryReasoningModel{}
	a, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{Name: "testTool", Parameters: map[string]interface{}{"type": "object"},
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"result": "success"}, nil
			}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	model.mu.Lock()
	prompts := model.prompts
	model.mu.Unlock()
	if len(prompts) != 2 {
		t.Fatalf("%d model calls, want 2", len(prompts))
	}
	var assistant []types.ContentPart
	for _, m := range prompts[1] {
		if m.Role == types.RoleAssistant {
			assistant = m.Content
		}
	}
	if len(assistant) != 3 {
		t.Fatalf("assistant content = %#v, want reasoning, text, tool-call", assistant)
	}
	r, ok := assistant[0].(types.ReasoningContent)
	if !ok || r.Text != "Inspect the configuration first." {
		t.Fatalf("part 0 = %#v, want the reasoning", assistant[0])
	}
	anthropic, _ := r.ProviderOptions["anthropic"].(map[string]interface{})
	openai, _ := r.ProviderOptions["openai"].(map[string]interface{})
	if anthropic["signature"] != "reasoning-signature" || openai["reasoningEncryptedContent"] != "encrypted-reasoning" {
		t.Errorf("reasoning providerOptions = %#v", r.ProviderOptions)
	}
	if txt, ok := assistant[1].(types.TextContent); !ok || txt.Text != "I will inspect the configuration." || txt.ProviderOptions != nil {
		t.Errorf("part 1 = %#v, want the text without providerOptions", assistant[1])
	}
	tc, ok := assistant[2].(types.ToolCallContent)
	if !ok || tc.ToolCallID != "call-1" {
		t.Fatalf("part 2 = %#v, want the tool call", assistant[2])
	}
	if o, _ := tc.ProviderOptions["openai"].(map[string]interface{}); o["itemId"] != "function-call-item" {
		t.Errorf("tool call providerOptions = %#v", tc.ProviderOptions)
	}
}

// r73carryFileModel streams a file, an (optionally empty) text, a source and
// a tool call, then answers "done", recording every call's prompt.
type r73carryFileModel struct {
	r73carryMetaModel
	text string
}

func (m *r73carryFileModel) DoStream(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	m.mu.Lock()
	m.prompts = append(m.prompts, append([]types.Message(nil), opts.Prompt.Messages...))
	n := len(m.prompts)
	m.mu.Unlock()
	if n == 1 {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "text/plain", Data: []byte("file-before-text")}},
			{Type: provider.ChunkTypeTextStart, ID: "t1"},
			{Type: provider.ChunkTypeText, ID: "t1", Text: m.text},
			{Type: provider.ChunkTypeTextEnd, ID: "t1"},
			{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "url", ID: "source-1", URL: "https://example.com/source", Title: "Example source"}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "testTool", Arguments: map[string]interface{}{"query": "test"}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}
	return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "done"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}), nil
}

// TS model-call-iterator.test.ts "preserves file and text order in a
// subsequent tool-call turn", "omits empty text from file-bearing tool-call
// message history" and "preserves sources in step results without adding
// them to message history": the next step's assistant message is the file,
// the text (left out when empty) and the tool call, with no source; the step
// keeps the source.
func TestR73Carry_FileTextOrderAndSourcesInHistory(t *testing.T) {
	for _, text := range []string{"Use this file.", ""} {
		model := &r73carryFileModel{text: text}
		a, err := NewWorkflowAgent(WorkflowAgent{
			Model: model,
			Tools: []types.Tool{{Name: "testTool", Parameters: map[string]interface{}{"type": "object"},
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return map[string]interface{}{"result": "success"}, nil
				}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "test"})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.ConsumeStream(); err != nil {
			t.Fatal(err)
		}
		steps := st.Steps()
		if len(steps) == 0 || len(steps[0].Sources) != 1 || steps[0].Sources[0].ID != "source-1" {
			t.Errorf("text=%q: first step sources = %#v, want source-1", text, steps)
		}
		model.mu.Lock()
		prompts := model.prompts
		model.mu.Unlock()
		if len(prompts) != 2 {
			t.Fatalf("text=%q: %d model calls, want 2", text, len(prompts))
		}
		var kinds []string
		for _, m := range prompts[1] {
			if m.Role != types.RoleAssistant {
				continue
			}
			for _, part := range m.Content {
				kinds = append(kinds, part.ContentType())
			}
		}
		want := []string{"file", "text", "tool-call"}
		if text == "" {
			want = []string{"file", "tool-call"}
		}
		if strings.Join(kinds, ",") != strings.Join(want, ",") {
			t.Errorf("text=%q: assistant parts = %v, want %v", text, kinds, want)
		}
	}
}

// r73carryInterleaveModel streams a provider-executed call, a client call and
// then the provider result, then answers "done", recording every prompt.
type r73carryInterleaveModel struct {
	r73carryMetaModel
}

func (m *r73carryInterleaveModel) DoStream(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	m.mu.Lock()
	m.prompts = append(m.prompts, append([]types.Message(nil), opts.Prompt.Messages...))
	n := len(m.prompts)
	m.mu.Unlock()
	if n == 1 {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "provider-call", ToolName: "providerTool", Arguments: map[string]interface{}{"query": "docs"}, ProviderExecuted: true}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "client-call", ToolName: "clientTool", Arguments: map[string]interface{}{"id": float64(1)}}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "provider-call", ToolName: "providerTool", Result: []interface{}{map[string]interface{}{"toolName": "clientTool"}}, ProviderExecuted: true}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}
	return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "done"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}), nil
}

// TS model-call-iterator.test.ts "interleaves provider-executed results while
// keeping client results in a tool message": the client result goes to the
// tool message and the provider result stays in the assistant message. A
// streamed provider result has a content position (TS
// providerExecutedToolResultPositions), so TS addToolResultsToConversation
// splices it in at that position: after the client call here, where it was
// emitted. (The TS case's fallback, a result right after its call, applies
// only to an iterator step whose content lacks the result part, which no Go
// stream produces.)
func TestR73Carry_ProviderResultInterleavedClientResultInToolMessage(t *testing.T) {
	model := &r73carryInterleaveModel{}
	a, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{
			{Name: "providerTool", Parameters: map[string]interface{}{"type": "object"}, ProviderExecuted: true},
			{Name: "clientTool", Parameters: map[string]interface{}{"type": "object"},
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return map[string]interface{}{"result": "success"}, nil
				}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	model.mu.Lock()
	prompts := model.prompts
	model.mu.Unlock()
	if len(prompts) != 2 {
		t.Fatalf("%d model calls, want 2", len(prompts))
	}
	var got []string
	for _, m := range prompts[1] {
		for _, part := range m.Content {
			id := ""
			switch p := part.(type) {
			case types.ToolCallContent:
				id = p.ToolCallID
			case types.ToolResultContent:
				id = p.ToolCallID
			}
			got = append(got, string(m.Role)+":"+part.ContentType()+":"+id)
		}
	}
	want := []string{"user:text:", "assistant:tool-call:provider-call", "assistant:tool-call:client-call", "assistant:tool-result:provider-call", "tool:tool-result:client-call"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("prompt = %v\nwant     %v", got, want)
	}
}

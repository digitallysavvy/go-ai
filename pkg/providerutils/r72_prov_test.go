package providerutils

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports of packages/ai/src/generate-text/to-response-messages.test.ts at
// e64ed82f27 (toResponseMessages). ConvertToResponseMessages takes the step
// content (TS inputContent); Go has no tools argument: a tool's toModelOutput
// result is already on the content part's Output when it reaches here.

func r72provRun(t *testing.T, content []types.ContentPart, want []types.Message) {
	t.Helper()
	got := ConvertToResponseMessages(nil, content, nil)
	if len(want) == 0 && len(got) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		gb, _ := json.MarshalIndent(got, "", " ")
		wb, _ := json.MarshalIndent(want, "", " ")
		t.Fatalf("response messages:\n got %s\nwant %s\n got %#v", gb, wb, got)
	}
}

func r72provAssistant(parts ...types.ContentPart) types.Message {
	return types.Message{Role: types.RoleAssistant, Content: parts}
}

func r72provTool(parts ...types.ContentPart) types.Message {
	return types.Message{Role: types.RoleTool, Content: parts}
}

func r72provMeta(provider, key string, value interface{}) json.RawMessage {
	b, _ := json.Marshal(map[string]interface{}{provider: map[string]interface{}{key: value}})
	return b
}

func r72provOpts(provider, key string, value interface{}) map[string]interface{} {
	return map[string]interface{}{provider: map[string]interface{}{key: value}}
}

func r72provEmpty() map[string]interface{} { return map[string]interface{}{} }

func r72provCall(id, name string, args map[string]interface{}) types.ToolCallContent {
	return types.ToolCallContent{ToolCallID: id, ToolName: name, Arguments: args}
}

func r72provOut(typ types.ToolResultOutputType, value interface{}) *types.ToolResultOutput {
	return &types.ToolResultOutput{Type: typ, Value: value}
}

var r72provPNG = []byte{137, 80, 78, 71, 13, 10, 26, 10}

func TestR72ProvToResponseMessages_TextOnly(t *testing.T) {
	r72provRun(t, []types.ContentPart{types.TextContent{Text: "Hello, world!"}},
		[]types.Message{r72provAssistant(types.TextContent{Text: "Hello, world!"})})
}

func TestR72ProvToResponseMessages_ToolCalls(t *testing.T) {
	r72provRun(t, []types.ContentPart{types.TextContent{Text: "Using a tool"}, r72provCall("123", "testTool", r72provEmpty())},
		[]types.Message{r72provAssistant(types.TextContent{Text: "Using a tool"}, r72provCall("123", "testTool", r72provEmpty()))})
}

func TestR72ProvToResponseMessages_ToolCallMetadata(t *testing.T) {
	call := r72provCall("123", "testTool", r72provEmpty())
	call.ProviderMetadata = r72provMeta("testProvider", "signature", "sig")
	want := r72provCall("123", "testTool", r72provEmpty())
	want.ProviderOptions = r72provOpts("testProvider", "signature", "sig")
	r72provRun(t, []types.ContentPart{types.TextContent{Text: "Using a tool"}, call},
		[]types.Message{r72provAssistant(types.TextContent{Text: "Using a tool"}, want)})
}

func TestR72ProvToResponseMessages_CustomParts(t *testing.T) {
	r72provRun(t, []types.ContentPart{types.CustomContent{Kind: "mock-provider.compaction", ProviderMetadata: r72provMeta("openai", "itemId", "cmp_123")}},
		[]types.Message{r72provAssistant(types.CustomContent{Kind: "mock-provider.compaction", ProviderOptions: r72provOpts("openai", "itemId", "cmp_123")})})
}

func TestR72ProvToResponseMessages_ToolResultsSeparateMessage(t *testing.T) {
	r72provRun(t, []types.ContentPart{
		types.TextContent{Text: "Tool used"},
		r72provCall("123", "testTool", r72provEmpty()),
		types.ToolResultContent{ToolCallID: "123", ToolName: "testTool", Result: "Tool result", Input: r72provEmpty()},
	}, []types.Message{
		r72provAssistant(types.TextContent{Text: "Tool used"}, r72provCall("123", "testTool", r72provEmpty())),
		r72provTool(types.ToolResultContent{ToolCallID: "123", ToolName: "testTool", Output: r72provOut(types.ToolResultOutputText, "Tool result")}),
	})
}

func TestR72ProvToResponseMessages_ToolErrorsSeparateMessage(t *testing.T) {
	r72provRun(t, []types.ContentPart{
		types.TextContent{Text: "Tool used"},
		r72provCall("123", "testTool", r72provEmpty()),
		types.ToolErrorContent{ToolCallID: "123", ToolName: "testTool", Error: "Tool error", Input: r72provEmpty()},
	}, []types.Message{
		r72provAssistant(types.TextContent{Text: "Tool used"}, r72provCall("123", "testTool", r72provEmpty())),
		r72provTool(types.ToolResultContent{ToolCallID: "123", ToolName: "testTool", Output: r72provOut(types.ToolResultOutputErrorText, "Tool error")}),
	})
}

func TestR72ProvToResponseMessages_ParallelResultsInCallOrder(t *testing.T) {
	r72provRun(t, []types.ContentPart{
		types.TextContent{Text: "Using tools"},
		r72provCall("call-a", "toolA", r72provEmpty()),
		r72provCall("call-b", "toolB", r72provEmpty()),
		types.ToolResultContent{ToolCallID: "call-b", ToolName: "toolB", Result: "B result", Input: r72provEmpty()},
		types.ToolResultContent{ToolCallID: "call-a", ToolName: "toolA", Result: "A result", Input: r72provEmpty()},
	}, []types.Message{
		r72provAssistant(types.TextContent{Text: "Using tools"}, r72provCall("call-a", "toolA", r72provEmpty()), r72provCall("call-b", "toolB", r72provEmpty())),
		r72provTool(
			types.ToolResultContent{ToolCallID: "call-a", ToolName: "toolA", Output: r72provOut(types.ToolResultOutputText, "A result")},
			types.ToolResultContent{ToolCallID: "call-b", ToolName: "toolB", Output: r72provOut(types.ToolResultOutputText, "B result")},
		),
	})
}

func TestR72ProvToResponseMessages_ReasoningOnly(t *testing.T) {
	r72provRun(t, []types.ContentPart{types.ReasoningContent{Text: "Thinking text", ProviderMetadata: r72provMeta("testProvider", "signature", "sig")}},
		[]types.Message{r72provAssistant(types.ReasoningContent{Text: "Thinking text", ProviderOptions: r72provOpts("testProvider", "signature", "sig")})})
}

func TestR72ProvToResponseMessages_RedactedReasoning(t *testing.T) {
	r72provRun(t, []types.ContentPart{
		types.ReasoningContent{Text: "redacted-data", ProviderMetadata: r72provMeta("testProvider", "isRedacted", true)},
		types.ReasoningContent{Text: "Thinking text", ProviderMetadata: r72provMeta("testProvider", "signature", "sig")},
		types.TextContent{Text: "Final text"},
	}, []types.Message{r72provAssistant(
		types.ReasoningContent{Text: "redacted-data", ProviderOptions: r72provOpts("testProvider", "isRedacted", true)},
		types.ReasoningContent{Text: "Thinking text", ProviderOptions: r72provOpts("testProvider", "signature", "sig")},
		types.TextContent{Text: "Final text"},
	)})
}

// TS 'should handle multipart tool results': toModelOutput's value is the
// output. In Go the tool's ToModelOutput result arrives as the part's Output.
func TestR72ProvToResponseMessages_ToModelOutput(t *testing.T) {
	proof := map[string]interface{}{"proof": "that toModelOutput is called"}
	r72provRun(t, []types.ContentPart{
		types.TextContent{Text: "multipart tool result"},
		r72provCall("123", "testTool", r72provEmpty()),
		types.ToolResultContent{ToolCallID: "123", ToolName: "testTool", Result: "image-base64", Output: r72provOut(types.ToolResultOutputJSON, proof), Input: r72provEmpty()},
	}, []types.Message{
		r72provAssistant(types.TextContent{Text: "multipart tool result"}, r72provCall("123", "testTool", r72provEmpty())),
		r72provTool(types.ToolResultContent{ToolCallID: "123", ToolName: "testTool", Output: r72provOut(types.ToolResultOutputJSON, proof)}),
	})
}

func TestR72ProvToResponseMessages_ReasoningFile(t *testing.T) {
	r72provRun(t, []types.ContentPart{
		types.ReasoningFileContent{Data: r72provPNG, MediaType: "image/png", ProviderMetadata: r72provMeta("testProvider", "signature", "sig")},
		types.TextContent{Text: "Here is my analysis"},
	}, []types.Message{r72provAssistant(
		types.ReasoningFileContent{Data: r72provPNG, MediaType: "image/png", ProviderOptions: r72provOpts("testProvider", "signature", "sig")},
		types.TextContent{Text: "Here is my analysis"},
	)})
	// The data serializes as TS's base64 "iVBORw0KGgo=".
	b, _ := json.Marshal(ConvertToResponseMessage(nil, []types.ContentPart{types.ReasoningFileContent{Data: r72provPNG, MediaType: "image/png"}}).Content[0])
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)
	if m["data"] != "iVBORw0KGgo=" || m["type"] != "reasoning-file" {
		t.Errorf("reasoning-file JSON = %s", b)
	}
}

func TestR72ProvToResponseMessages_Images(t *testing.T) {
	jpeg := []byte{255, 216, 255}
	r72provRun(t, []types.ContentPart{
		types.TextContent{Text: "Here are multiple images"},
		types.GeneratedFileContent{Data: r72provPNG, MediaType: "image/png"},
		types.GeneratedFileContent{Data: jpeg, MediaType: "image/jpeg"},
	}, []types.Message{r72provAssistant(
		types.TextContent{Text: "Here are multiple images"},
		types.FileContent{Data: r72provPNG, MediaType: "image/png"},
		types.FileContent{Data: jpeg, MediaType: "image/jpeg"},
	)})
}

func TestR72ProvToResponseMessages_ImagesReasoningToolCallsOrder(t *testing.T) {
	r72provRun(t, []types.ContentPart{
		types.ReasoningContent{Text: "Thinking text", ProviderMetadata: r72provMeta("testProvider", "signature", "sig")},
		types.GeneratedFileContent{Data: r72provPNG, MediaType: "image/png"},
		types.TextContent{Text: "Combined response"},
		r72provCall("123", "testTool", r72provEmpty()),
	}, []types.Message{r72provAssistant(
		types.ReasoningContent{Text: "Thinking text", ProviderOptions: r72provOpts("testProvider", "signature", "sig")},
		types.FileContent{Data: r72provPNG, MediaType: "image/png"},
		types.TextContent{Text: "Combined response"},
		r72provCall("123", "testTool", r72provEmpty()),
	)})
}

func TestR72ProvToResponseMessages_EmptyTextAndNoContent(t *testing.T) {
	r72provRun(t, []types.ContentPart{types.TextContent{Text: ""}, r72provCall("123", "testTool", r72provEmpty())},
		[]types.Message{r72provAssistant(r72provCall("123", "testTool", r72provEmpty()))})
	if got := ConvertToResponseMessages(nil, nil, nil); len(got) != 0 {
		t.Errorf("no content: %#v, want no messages", got)
	}
}

func TestR72ProvToResponseMessages_ProviderExecutedCallAndResult(t *testing.T) {
	input := map[string]interface{}{"query": "San Francisco major news events June 22 2025"}
	output := []interface{}{map[string]interface{}{"url": "https://patch.com/california/san-francisco/calendar"}}
	call := r72provCall("srvtoolu_011cNtbtzFARKPcAcp7w4nh9", "web_search", input)
	call.ProviderExecuted = true
	r72provRun(t, []types.ContentPart{
		types.TextContent{Text: "Let me search for recent news from San Francisco."},
		call,
		types.ToolResultContent{ToolCallID: "srvtoolu_011cNtbtzFARKPcAcp7w4nh9", ToolName: "web_search", Input: input, Result: output, ProviderExecuted: true},
		types.TextContent{Text: "Based on the search results"},
	}, []types.Message{r72provAssistant(
		types.TextContent{Text: "Let me search for recent news from San Francisco."},
		call,
		types.ToolResultContent{ToolCallID: "srvtoolu_011cNtbtzFARKPcAcp7w4nh9", ToolName: "web_search", Output: r72provOut(types.ToolResultOutputJSON, output)},
		types.TextContent{Text: "Based on the search results"},
	)})
}

// TS 'should preserve schema input when the approved input was transformed',
// and the other half of the same rule (isDeepEqualData): an inputSchemaInput
// equal to the call input is not repeated.
func TestR72ProvToResponseMessages_ApprovalInputSchemaInput(t *testing.T) {
	call := r72provCall("count-call", "count", map[string]interface{}{"count": float64(3)})
	request := types.ToolApprovalRequestContent{ApprovalID: "approval-1",
		ToolCall:         types.ToolCall{ID: "count-call", ToolName: "count", Arguments: map[string]interface{}{"count": float64(3)}},
		InputSchemaInput: map[string]interface{}{"count": "3"}}
	r72provRun(t, []types.ContentPart{call, request}, []types.Message{r72provAssistant(call,
		types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "count-call", InputSchemaInput: map[string]interface{}{"count": "3"}})})

	request.InputSchemaInput = map[string]interface{}{"count": float64(3)}
	r72provRun(t, []types.ContentPart{call, request}, []types.Message{r72provAssistant(call,
		types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "count-call"})})
}

func TestR72ProvToResponseMessages_ApprovalRequests(t *testing.T) {
	tokyo := map[string]interface{}{"city": "Tokyo"}
	weatherCall := types.ToolCall{ID: "123", ToolName: "weather", Arguments: tokyo}
	r72provRun(t, []types.ContentPart{
		types.TextContent{Text: "Let me check the weather"},
		r72provCall("123", "weather", tokyo),
		types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCall: weatherCall},
	}, []types.Message{r72provAssistant(
		types.TextContent{Text: "Let me check the weather"},
		r72provCall("123", "weather", tokyo),
		types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "123"},
	)})

	// Provider-executed (dynamic MCP) call.
	query := map[string]interface{}{"query": "test"}
	mcpCall := types.ToolCallContent{ToolCallID: "mcp-call-1", ToolName: "mcp_tool", Arguments: query, ProviderExecuted: true, Dynamic: true}
	wantCall := mcpCall
	wantCall.Dynamic = false
	r72provRun(t, []types.ContentPart{
		mcpCall,
		types.ToolApprovalRequestContent{ApprovalID: "mcp-approval-1", ToolCall: types.ToolCall{ID: "mcp-call-1", ToolName: "mcp_tool", Arguments: query, ProviderExecuted: true, Dynamic: true}},
	}, []types.Message{r72provAssistant(wantCall, types.ToolApprovalRequestContent{ApprovalID: "mcp-approval-1", ToolCallID: "mcp-call-1"})})

	// Automatic approval keeps isAutomatic.
	r72provRun(t, []types.ContentPart{
		r72provCall("call-1", "weather", tokyo),
		types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCall: types.ToolCall{ID: "call-1", ToolName: "weather", Arguments: tokyo}, IsAutomatic: true},
	}, []types.Message{r72provAssistant(
		r72provCall("call-1", "weather", tokyo),
		types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1", IsAutomatic: true},
	)})
}

func TestR72ProvToResponseMessages_ApprovalResponseStages(t *testing.T) {
	tokyo := map[string]interface{}{"city": "Tokyo"}
	call := types.ToolCall{ID: "call-1", ToolName: "weather", Arguments: tokyo}
	request := types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCall: call, IsAutomatic: true}
	r72provRun(t, []types.ContentPart{
		r72provCall("call-1", "weather", tokyo),
		request,
		types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true, ToolCall: call},
		types.ToolResultContent{ToolCallID: "call-1", ToolName: "weather", Input: tokyo, Result: "72F and sunny"},
	}, []types.Message{
		r72provAssistant(r72provCall("call-1", "weather", tokyo), types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1", IsAutomatic: true}),
		r72provTool(
			types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true},
			types.ToolResultContent{ToolCallID: "call-1", ToolName: "weather", Output: r72provOut(types.ToolResultOutputText, "72F and sunny")},
		),
	})

	// Denied: the approval response and an execution-denied result.
	r72provRun(t, []types.ContentPart{
		r72provCall("call-1", "weather", tokyo),
		types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCall: call},
		types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: false, Reason: "User denied access", ToolCall: call},
	}, []types.Message{
		r72provAssistant(r72provCall("call-1", "weather", tokyo), types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1"}),
		r72provTool(
			types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: false, Reason: "User denied access"},
			types.ToolResultContent{ToolCallID: "call-1", ToolName: "weather", Output: &types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied, Reason: "User denied access"}},
		),
	})
}

func TestR72ProvToResponseMessages_ProviderExecutedApprovalAndResult(t *testing.T) {
	query := map[string]interface{}{"query": "test"}
	mcpCall := types.ToolCallContent{ToolCallID: "mcp-call-1", ToolName: "mcp_tool", Arguments: query, ProviderExecuted: true, Dynamic: true}
	wantCall := mcpCall
	wantCall.Dynamic = false
	toolCall := types.ToolCall{ID: "mcp-call-1", ToolName: "mcp_tool", Arguments: query, ProviderExecuted: true, Dynamic: true}
	r72provRun(t, []types.ContentPart{
		mcpCall,
		types.ToolApprovalRequestContent{ApprovalID: "mcp-approval-1", ToolCall: toolCall},
		types.ToolApprovalResponseContent{ApprovalID: "mcp-approval-1", Approved: true, ProviderExecuted: true, ProviderExecutedSet: true, ToolCall: toolCall},
	}, []types.Message{
		r72provAssistant(wantCall, types.ToolApprovalRequestContent{ApprovalID: "mcp-approval-1", ToolCallID: "mcp-call-1"}),
		r72provTool(types.ToolApprovalResponseContent{ApprovalID: "mcp-approval-1", Approved: true, ProviderExecuted: true, ProviderExecutedSet: true}),
	})

	// A provider-executed result stays in the assistant message only.
	value := map[string]interface{}{"value": "provider result"}
	r72provRun(t, []types.ContentPart{
		mcpCall,
		types.ToolResultContent{ToolCallID: "mcp-call-1", ToolName: "mcp_tool", Input: query, Result: value, ProviderExecuted: true, Dynamic: true},
	}, []types.Message{r72provAssistant(wantCall, types.ToolResultContent{ToolCallID: "mcp-call-1", ToolName: "mcp_tool", Output: r72provOut(types.ToolResultOutputJSON, value)})})
}

func TestR72ProvToResponseMessages_InvalidToolCalls(t *testing.T) {
	// Non-object input: the replayed call input is {}.
	r72provRun(t, []types.ContentPart{
		types.ToolCallContent{ToolCallID: "call-1", ToolName: "weather", Input: "{ city: San Francisco, }", Dynamic: true, Invalid: true, Error: errors.New("JSON parsing failed")},
		types.ToolErrorContent{ToolCallID: "call-1", ToolName: "weather", Error: "Invalid input for tool weather: JSON parsing failed", Dynamic: true},
	}, []types.Message{
		r72provAssistant(types.ToolCallContent{ToolCallID: "call-1", ToolName: "weather", Input: "{ city: San Francisco, }", Arguments: r72provEmpty()}),
		r72provTool(types.ToolResultContent{ToolCallID: "call-1", ToolName: "weather", Output: r72provOut(types.ToolResultOutputErrorText, "Invalid input for tool weather: JSON parsing failed")}),
	})

	// A valid object input on an invalid call is kept.
	cities := map[string]interface{}{"cities": "San Francisco"}
	r72provRun(t, []types.ContentPart{
		types.ToolCallContent{ToolCallID: "call-1", ToolName: "weather", Arguments: cities, Dynamic: true, Invalid: true, Error: errors.New("Type validation failed")},
		types.ToolErrorContent{ToolCallID: "call-1", ToolName: "weather", Input: cities, Error: "Invalid input for tool weather: Type validation failed", Dynamic: true},
	}, []types.Message{
		r72provAssistant(r72provCall("call-1", "weather", cities)),
		r72provTool(types.ToolResultContent{ToolCallID: "call-1", ToolName: "weather", Output: r72provOut(types.ToolResultOutputErrorText, "Invalid input for tool weather: Type validation failed")}),
	})
}

func TestR72ProvToResponseMessages_TextProviderMetadata(t *testing.T) {
	r72provRun(t, []types.ContentPart{types.TextContent{Text: "Here is a text", ProviderMetadata: r72provMeta("testProvider", "signature", "sig")}},
		[]types.Message{r72provAssistant(types.TextContent{Text: "Here is a text", ProviderOptions: r72provOpts("testProvider", "signature", "sig")})})
}

// TS createToolModelOutput errorMode 'text' is getErrorMessage(error): the
// string itself, "unknown error" for a missing error, error.toString() for an
// Error and JSON.stringify for anything else; the error-text value is always
// a string.
func TestR72ProvToResponseMessages_ToolErrorTextIsGetErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		part types.ToolErrorContent
		want string
	}{
		{"string", types.ToolErrorContent{Error: "boom"}, "boom"},
		{"error text wins", types.ToolErrorContent{Error: "boom", ErrorText: "Error: boom"}, "Error: boom"},
		{"nil", types.ToolErrorContent{}, "unknown error"},
		{"error value", types.ToolErrorContent{Error: errors.New("boom")}, "Error: boom"},
		{"object", types.ToolErrorContent{Error: map[string]interface{}{"code": "E1", "retry": false}}, `{"code":"E1","retry":false}`},
	}
	for _, c := range cases {
		c.part.ToolCallID, c.part.ToolName = "call-1", "t"
		got := ConvertToResponseMessages(nil, []types.ContentPart{c.part}, nil)
		want := []types.Message{r72provTool(types.ToolResultContent{ToolCallID: "call-1", ToolName: "t", Output: r72provOut(types.ToolResultOutputErrorText, c.want)})}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %#v, want error-text %q", c.name, got[0].Content, c.want)
		}
	}
}

// TS errorMode 'json' (a provider-executed tool-error in the assistant
// message) is toJSONValue(part.error): never the error text, and a plain
// JSON value.
func TestR72ProvToResponseMessages_ProviderToolErrorJSONIsToJSONValue(t *testing.T) {
	type r72provErrBody struct {
		Type      string `json:"type"`
		ErrorCode string `json:"errorCode"`
	}
	part := types.ToolErrorContent{ToolCallID: "srv-1", ToolName: "web_search", ProviderExecuted: true,
		Error: r72provErrBody{Type: "web_search_tool_result_error", ErrorCode: "max_uses_exceeded"}, ErrorText: "Error: ignored"}
	got := ConvertToResponseMessages(nil, []types.ContentPart{part}, nil)
	want := []types.Message{r72provAssistant(types.ToolResultContent{ToolCallID: "srv-1", ToolName: "web_search",
		Output: r72provOut(types.ToolResultOutputErrorJSON, map[string]interface{}{"type": "web_search_tool_result_error", "errorCode": "max_uses_exceeded"})})}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got[0].Content, want[0].Content)
	}
	part.Error = nil
	got = ConvertToResponseMessages(nil, []types.ContentPart{part}, nil)
	if out := got[0].Content[0].(types.ToolResultContent).Output; out.Type != types.ToolResultOutputErrorJSON || out.Value != nil {
		t.Errorf("nil provider error: output %#v, want error-json null (TS toJSONValue(undefined))", out)
	}
}

// The inputSchemaInput comparison is on JSON data: a raw JSON form equal to
// the call input is dropped too, and a different one is kept.
func TestR72ProvToResponseMessages_ApprovalInputSchemaInputRawJSON(t *testing.T) {
	args := map[string]interface{}{"b": float64(2), "a": "x"}
	for _, c := range []struct {
		raw  string
		keep bool
	}{{`{"a":"x","b":2.0}`, false}, {`{"a":"x","b":"2"}`, true}} {
		request := types.ToolApprovalRequestContent{ApprovalID: "ap", ToolCall: types.ToolCall{ID: "c1", ToolName: "t", Arguments: args}, InputSchemaInput: json.RawMessage(c.raw)}
		got := ConvertToResponseMessages(nil, []types.ContentPart{r72provCall("c1", "t", args), request}, nil)
		kept := got[0].Content[1].(types.ToolApprovalRequestContent).InputSchemaInput != nil
		if kept != c.keep {
			t.Errorf("%s: inputSchemaInput kept=%v, want %v", c.raw, kept, c.keep)
		}
	}
}

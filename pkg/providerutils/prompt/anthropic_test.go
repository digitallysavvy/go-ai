package prompt

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Tests ported from packages/anthropic/src/convert-to-anthropic-prompt.test.ts
// (ai@7.0.113). Expected values are the TS fixtures with `undefined` fields
// removed (they are dropped by JSON serialization).

func anthropicOpt(kv ...interface{}) map[string]interface{} {
	inner := map[string]interface{}{}
	for i := 0; i+1 < len(kv); i += 2 {
		inner[kv[i].(string)] = kv[i+1]
	}
	return map[string]interface{}{"anthropic": inner}
}

var ephemeral = map[string]interface{}{"type": "ephemeral"}

func assertJSONEqual(t *testing.T, label string, got interface{}, wantJSON string) {
	t.Helper()
	gotBytes, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("%s: marshal: %v", label, err)
	}
	var gotV, wantV interface{}
	if err := json.Unmarshal(gotBytes, &gotV); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(wantJSON), &wantV); err != nil {
		t.Fatalf("%s: bad want JSON: %v", label, err)
	}
	if !reflect.DeepEqual(gotV, wantV) {
		want, _ := json.MarshalIndent(wantV, "", "  ")
		gotPretty, _ := json.MarshalIndent(gotV, "", "  ")
		t.Errorf("%s mismatch\n got: %s\nwant: %s", label, gotPretty, want)
	}
}

func warningMessages(ws []types.Warning) []string {
	out := []string{}
	for _, w := range ws {
		if w.Message != "" {
			out = append(out, w.Message)
		} else {
			out = append(out, w.Details)
		}
	}
	return out
}

func userText(text string) types.Message {
	return types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: text}}}
}

func systemText(text string, providerOptions map[string]interface{}) types.Message {
	return types.Message{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: text}}, ProviderOptions: providerOptions}
}

func TestConvertToAnthropicPrompt_Golden(t *testing.T) {
	falseVal := false
	tests := []struct {
		name         string
		messages     []types.Message
		opts         AnthropicPromptOptions
		wantMessages string
		wantSystem   string // "" means no system
		wantBetas    []string
		wantWarnings []string
	}{
		// ── system messages ─────────────────────────────────────────────
		{
			name:         "single system message",
			messages:     []types.Message{systemText("This is a system message", nil)},
			wantMessages: `[]`,
			wantSystem:   `[{"type":"text","text":"This is a system message"}]`,
		},
		{
			name: "multiple system messages",
			messages: []types.Message{
				systemText("This is a system message", nil),
				systemText("This is another system message", nil),
			},
			wantMessages: `[]`,
			wantSystem:   `[{"type":"text","text":"This is a system message"},{"type":"text","text":"This is another system message"}]`,
		},
		{
			name: "mid-conversation system message inline + beta",
			messages: []types.Message{
				systemText("initial", nil),
				userText("hi"),
				{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "hello"}}},
				systemText("switch tone", nil),
				userText("go"),
			},
			wantSystem: `[{"type":"text","text":"initial"}]`,
			wantMessages: `[
				{"role":"user","content":[{"type":"text","text":"hi"}]},
				{"role":"assistant","content":[{"type":"text","text":"hello"}]},
				{"role":"system","content":[{"type":"text","text":"switch tone"}]},
				{"role":"user","content":[{"type":"text","text":"go"}]}
			]`,
			wantBetas: []string{AnthropicBetaMidConversationSystem},
		},
		{
			name: "clearAt and effort on mid-conversation system messages",
			messages: []types.Message{
				systemText("initial", nil),
				userText("hi"),
				{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "hello"}}},
				systemText("", anthropicOpt("clearAt", "next_user_message", "effort", "high")),
				systemText("this instruction persists", nil),
				userText("go"),
			},
			wantSystem: `[{"type":"text","text":"initial"}]`,
			wantMessages: `[
				{"role":"user","content":[{"type":"text","text":"hi"}]},
				{"role":"assistant","content":[{"type":"text","text":"hello"}]},
				{"role":"system","content":[],"clear_at":"next_user_message","output_config":{"effort":"high"}},
				{"role":"system","content":[{"type":"text","text":"this instruction persists"}]},
				{"role":"user","content":[{"type":"text","text":"go"}]}
			]`,
			wantBetas: []string{AnthropicBetaMidConversationSystem, AnthropicBetaMidConversationClearAt, AnthropicBetaMidConversationOutputCfg},
		},
		{
			name: "tool change blocks on mid-conversation system message",
			messages: []types.Message{
				systemText("initial", nil),
				userText("hi"),
				systemText("", anthropicOpt("toolChanges", []interface{}{
					map[string]interface{}{"type": "tool_addition", "toolName": "weather"},
					map[string]interface{}{"type": "tool_removal", "toolName": "search"},
				})),
			},
			wantSystem: `[{"type":"text","text":"initial"}]`,
			wantMessages: `[
				{"role":"user","content":[{"type":"text","text":"hi"}]},
				{"role":"system","content":[
					{"type":"tool_addition","tool":{"type":"tool_reference","name":"weather"}},
					{"type":"tool_removal","tool":{"type":"tool_reference","name":"search"}}
				]}
			]`,
			wantBetas: []string{AnthropicBetaMidConversationSystem, AnthropicBetaMidConversationToolChanges},
		},
		{
			name: "warn and drop tool changes on the initial system message",
			messages: []types.Message{
				systemText("initial", anthropicOpt("toolChanges", []interface{}{
					map[string]interface{}{"type": "tool_addition", "toolName": "weather"},
				})),
				userText("hi"),
			},
			wantSystem:   `[{"type":"text","text":"initial"}]`,
			wantMessages: `[{"role":"user","content":[{"type":"text","text":"hi"}]}]`,
			wantWarnings: []string{"tool changes on the initial system message are not supported by Anthropic. Configure the initial tool set via the tools option instead. The tool changes have been ignored."},
		},

		// ── effort-only system messages (TS 67f800090a) ───────────────────
		// TS: 'should preserve initial effort messages alone'.
		{
			name: "preserve initial effort messages alone",
			messages: []types.Message{
				systemText("", anthropicOpt("effort", "low")),
				userText("hi"),
			},
			wantSystem: `[]`,
			wantMessages: `[
				{"role":"system","content":[],"output_config":{"effort":"low"}},
				{"role":"user","content":[{"type":"text","text":"hi"}]}
			]`,
			wantBetas: []string{AnthropicBetaMidConversationOutputCfg},
		},
		// TS: 'should preserve initial effort messages after initial instructions'.
		{
			name: "preserve initial effort messages after initial instructions",
			messages: []types.Message{
				systemText("initial", nil),
				systemText("", anthropicOpt("effort", "low")),
				userText("hi"),
			},
			wantSystem: `[{"type":"text","text":"initial"}]`,
			wantMessages: `[
				{"role":"system","content":[],"output_config":{"effort":"low"}},
				{"role":"user","content":[{"type":"text","text":"hi"}]}
			]`,
			wantBetas: []string{AnthropicBetaMidConversationOutputCfg},
		},
		// TS: 'should preserve initial effort messages before initial instructions'.
		{
			name: "preserve initial effort messages before initial instructions",
			messages: []types.Message{
				systemText("", anthropicOpt("effort", "low")),
				systemText("initial", nil),
				userText("hi"),
			},
			wantSystem: `[{"type":"text","text":"initial"}]`,
			wantMessages: `[
				{"role":"system","content":[],"output_config":{"effort":"low"}},
				{"role":"user","content":[{"type":"text","text":"hi"}]}
			]`,
			wantBetas: []string{AnthropicBetaMidConversationOutputCfg},
		},
		// TS: 'should preserve initial effort messages consecutively'.
		{
			name: "preserve initial effort messages consecutively",
			messages: []types.Message{
				systemText("", anthropicOpt("effort", "low")),
				systemText("", anthropicOpt("effort", "high")),
				userText("hi"),
			},
			wantSystem: `[]`,
			wantMessages: `[
				{"role":"system","content":[],"output_config":{"effort":"low"}},
				{"role":"system","content":[],"output_config":{"effort":"high"}},
				{"role":"user","content":[{"type":"text","text":"hi"}]}
			]`,
			wantBetas: []string{AnthropicBetaMidConversationOutputCfg},
		},
		// TS: 'should preserve initial effort messages around initial instructions'.
		{
			name: "preserve initial effort messages around initial instructions",
			messages: []types.Message{
				systemText("", anthropicOpt("effort", "low")),
				systemText("initial", nil),
				systemText("", anthropicOpt("effort", "high")),
				userText("hi"),
			},
			wantSystem: `[{"type":"text","text":"initial"}]`,
			wantMessages: `[
				{"role":"system","content":[],"output_config":{"effort":"low"}},
				{"role":"system","content":[],"output_config":{"effort":"high"}},
				{"role":"user","content":[{"type":"text","text":"hi"}]}
			]`,
			wantBetas: []string{AnthropicBetaMidConversationOutputCfg},
		},
		// TS: 'should preserve later consecutive effort messages with initial
		// instructions: false' (no leading system message).
		{
			name: "preserve later consecutive effort messages without initial instructions",
			messages: []types.Message{
				userText("hi"),
				systemText("", anthropicOpt("effort", "low")),
				systemText("", anthropicOpt("effort", "high")),
				systemText("later instructions", nil),
			},
			wantSystem: "",
			wantMessages: `[
				{"role":"user","content":[{"type":"text","text":"hi"}]},
				{"role":"system","content":[],"output_config":{"effort":"low"}},
				{"role":"system","content":[],"output_config":{"effort":"high"}},
				{"role":"system","content":[{"type":"text","text":"later instructions"}]}
			]`,
			wantBetas: []string{AnthropicBetaMidConversationSystem, AnthropicBetaMidConversationOutputCfg},
		},
		// TS: 'should preserve later consecutive effort messages with initial
		// instructions: true'.
		{
			name: "preserve later consecutive effort messages with initial instructions",
			messages: []types.Message{
				systemText("initial", nil),
				userText("hi"),
				systemText("", anthropicOpt("effort", "low")),
				systemText("", anthropicOpt("effort", "high")),
				systemText("later instructions", nil),
			},
			wantSystem: `[{"type":"text","text":"initial"}]`,
			wantMessages: `[
				{"role":"user","content":[{"type":"text","text":"hi"}]},
				{"role":"system","content":[],"output_config":{"effort":"low"}},
				{"role":"system","content":[],"output_config":{"effort":"high"}},
				{"role":"system","content":[{"type":"text","text":"later instructions"}]}
			]`,
			wantBetas: []string{AnthropicBetaMidConversationSystem, AnthropicBetaMidConversationOutputCfg},
		},
		// TS: 'should warn and ignore unsupported initial system options' (text + effort).
		{
			name: "warn and ignore unsupported initial system options: text plus effort",
			messages: []types.Message{
				systemText("initial", anthropicOpt("effort", "low")),
				userText("hi"),
			},
			wantSystem:   `[{"type":"text","text":"initial"}]`,
			wantMessages: `[{"role":"user","content":[{"type":"text","text":"hi"}]}]`,
			wantWarnings: []string{"clearAt and effort on this initial system message are not supported by Anthropic. Use a separate effort-only system message with empty content to set effort. These options have been ignored."},
		},
		// TS: 'should warn and ignore unsupported initial system options' (clearAt only).
		{
			name: "warn and ignore unsupported initial system options: clearAt only",
			messages: []types.Message{
				systemText("", anthropicOpt("clearAt", "next_user_message")),
				userText("hi"),
			},
			wantSystem:   `[]`,
			wantMessages: `[{"role":"user","content":[{"type":"text","text":"hi"}]}]`,
			wantWarnings: []string{"clearAt and effort on this initial system message are not supported by Anthropic. Use a separate effort-only system message with empty content to set effort. These options have been ignored."},
		},
		// TS: 'should warn and ignore unsupported initial system options' (clearAt + effort).
		{
			name: "warn and ignore unsupported initial system options: clearAt plus effort",
			messages: []types.Message{
				systemText("", anthropicOpt("clearAt", "next_user_message", "effort", "low")),
				userText("hi"),
			},
			wantSystem:   `[]`,
			wantMessages: `[{"role":"user","content":[{"type":"text","text":"hi"}]}]`,
			wantWarnings: []string{"clearAt and effort on this initial system message are not supported by Anthropic. Use a separate effort-only system message with empty content to set effort. These options have been ignored."},
		},

		// ── user messages ───────────────────────────────────────────────
		{
			name: "image parts for byte images",
			messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
				types.FileContent{Data: []byte{0, 1, 2, 3}, MediaType: "image/png"},
			}}},
			wantMessages: `[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAECAw=="}}]}]`,
		},
		{
			name: "image parts for URL images",
			messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
				types.FileContent{URL: "https://example.com/image.png", MediaType: "image/*"},
			}}},
			wantMessages: `[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://example.com/image.png"}}]}]`,
		},
		{
			name: "legacy ImageContent maps to an image block",
			messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
				types.ImageContent{Image: []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, MimeType: "image"},
			}}},
			wantMessages: `[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}}]}]`,
		},
		{
			name: "PDF file parts for base64 PDFs",
			messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
				types.FileContent{Data: []byte("base64PDFdata"), MediaType: "application/pdf"},
			}}},
			wantMessages: `[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"YmFzZTY0UERGZGF0YQ=="}}]}]`,
			wantBetas:    []string{AnthropicBetaPDFs},
		},
		{
			name: "PDF file parts for URL PDFs",
			messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
				types.FileContent{URL: "https://example.com/document.pdf", MediaType: "application/pdf"},
			}}},
			wantMessages: `[{"role":"user","content":[{"type":"document","source":{"type":"url","url":"https://example.com/document.pdf"}}]}]`,
			wantBetas:    []string{AnthropicBetaPDFs},
		},
		{
			name: "text file parts for text/plain documents",
			messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
				types.FileContent{Data: []byte("sample text content"), MediaType: "text/plain", Filename: "sample.txt"},
			}}},
			wantMessages: `[{"role":"user","content":[{"type":"document","source":{"type":"text","media_type":"text/plain","data":"sample text content"},"title":"sample.txt"}]}]`,
		},
		{
			name: "inline text file parts map to inline text document source",
			messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
				types.FileContent{FileData: types.FileData{Type: types.FileDataTypeText, Text: "inline"}, MediaType: "text/plain"},
			}}},
			wantMessages: `[{"role":"user","content":[{"type":"document","source":{"type":"text","media_type":"text/plain","data":"inline"}}]}]`,
		},
		{
			name: "image file parts using provider reference",
			messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
				types.FileContent{FileData: types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"anthropic": "file_abc"}}, MediaType: "image/png"},
			}}},
			wantMessages: `[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"file_abc"}}]}]`,
			wantBetas:    []string{AnthropicBetaFilesAPI},
		},
		{
			name: "container upload for provider referenced files",
			messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
				types.FileContent{
					FileData:        types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"anthropic": "file_abc"}},
					MediaType:       "text/csv",
					ProviderOptions: anthropicOpt("containerUpload", true),
				},
			}}},
			wantMessages: `[{"role":"user","content":[{"type":"container_upload","file_id":"file_abc"}]}]`,
			wantBetas:    []string{AnthropicBetaFilesAPI},
		},

		// ── tool messages ───────────────────────────────────────────────
		{
			name: "single tool result becomes a user message",
			messages: []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{
				types.ToolResultContent{ToolName: "tool-1", ToolCallID: "tool-call-1", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"test": "This is a tool message"}}},
			}}},
			wantMessages: `[{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool-call-1","content":"{\"test\":\"This is a tool message\"}"}]}]`,
		},
		{
			name: "combine user and tool messages",
			messages: []types.Message{
				{Role: types.RoleTool, Content: []types.ContentPart{
					types.ToolResultContent{ToolName: "tool-1", ToolCallID: "tool-call-1", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"test": "This is a tool message"}}},
				}},
				userText("This is a user message"),
			},
			wantMessages: `[{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"tool-call-1","content":"{\"test\":\"This is a tool message\"}"},
				{"type":"text","text":"This is a user message"}
			]}]`,
		},
		{
			name: "tool result with content parts",
			messages: []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{
				types.ToolResultContent{ToolName: "image-generator", ToolCallID: "image-gen-1", Output: &types.ToolResultOutput{
					Type: types.ToolResultOutputContent,
					Content: []types.ToolResultContentBlock{
						types.TextContentBlock{Text: "Image generated successfully"},
						types.FileContentBlock{Data: []byte{0, 1, 2, 3}, MediaType: "image/png"},
					},
				}},
			}}},
			wantMessages: `[{"role":"user","content":[{"type":"tool_result","tool_use_id":"image-gen-1","content":[
				{"type":"text","text":"Image generated successfully"},
				{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAECAw=="}}
			]}]}]`,
		},
		{
			name: "tool result with custom tool-reference content",
			messages: []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{
				types.ToolResultContent{ToolName: "searchTools", ToolCallID: "search-1", Output: &types.ToolResultOutput{
					Type: types.ToolResultOutputContent,
					Content: []types.ToolResultContentBlock{
						types.CustomContentBlock{ProviderOptions: anthropicOpt("type", "tool-reference", "toolName", "get_weather")},
						types.CustomContentBlock{ProviderOptions: anthropicOpt("type", "tool-reference", "toolName", "get_forecast")},
					},
				}},
			}}},
			wantMessages: `[{"role":"user","content":[{"type":"tool_result","tool_use_id":"search-1","content":[
				{"type":"tool_reference","tool_name":"get_weather"},
				{"type":"tool_reference","tool_name":"get_forecast"}
			]}]}]`,
		},
		{
			name: "tool result with url-based PDF content",
			messages: []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{
				types.ToolResultContent{ToolName: "pdf", ToolCallID: "pdf-1", Output: &types.ToolResultOutput{
					Type:    types.ToolResultOutputContent,
					Content: []types.ToolResultContentBlock{types.FileContentBlock{URL: "https://example.com/doc.pdf", MediaType: "application/pdf"}},
				}},
			}}},
			wantMessages: `[{"role":"user","content":[{"type":"tool_result","tool_use_id":"pdf-1","content":[{"type":"document","source":{"type":"url","url":"https://example.com/doc.pdf"}}]}]}]`,
		},
		{
			name: "error-text and execution-denied tool results",
			messages: []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{
				types.ToolResultContent{ToolName: "t", ToolCallID: "c1", Output: &types.ToolResultOutput{Type: types.ToolResultOutputErrorText, Value: "boom"}},
				types.ToolResultContent{ToolName: "t", ToolCallID: "c2", Output: &types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied}},
				types.ToolResultContent{ToolName: "t", ToolCallID: "c3", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "plain"}},
			}}},
			wantMessages: `[{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"c1","content":"boom","is_error":true},
				{"type":"tool_result","tool_use_id":"c2","content":"Tool call execution denied."},
				{"type":"tool_result","tool_use_id":"c3","content":"plain"}
			]}]`,
		},

		// ── assistant messages ──────────────────────────────────────────
		{
			name: "omit empty compaction blocks",
			messages: []types.Message{
				userText("hi"),
				{Role: types.RoleAssistant, Content: []types.ContentPart{
					types.TextContent{Text: "", ProviderOptions: anthropicOpt("type", "compaction")},
					types.TextContent{Text: "answer"},
				}},
			},
			wantMessages: `[{"role":"user","content":[{"type":"text","text":"hi"}]},{"role":"assistant","content":[{"type":"text","text":"answer"}]}]`,
		},
		{
			name: "preserve non-empty compaction blocks",
			messages: []types.Message{
				{Role: types.RoleAssistant, Content: []types.ContentPart{
					types.TextContent{Text: "summary", ProviderOptions: anthropicOpt("type", "compaction", "signature", "sig")},
				}},
			},
			wantMessages: `[{"role":"assistant","content":[{"type":"compaction","content":"summary","signature":"sig"}]}]`,
			wantBetas:    []string{AnthropicBetaCompact},
		},
		{
			name: "remove trailing whitespace from last assistant message",
			messages: []types.Message{
				userText("user content"),
				{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "assistant content  "}}},
			},
			wantMessages: `[{"role":"user","content":[{"type":"text","text":"user content"}]},{"role":"assistant","content":[{"type":"text","text":"assistant content"}]}]`,
		},
		{
			name: "keep trailing whitespace when a user message follows",
			messages: []types.Message{
				userText("user content"),
				{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "assistant content  "}}},
				userText("user content 2"),
			},
			wantMessages: `[
				{"role":"user","content":[{"type":"text","text":"user content"}]},
				{"role":"assistant","content":[{"type":"text","text":"assistant content  "}]},
				{"role":"user","content":[{"type":"text","text":"user content 2"}]}
			]`,
		},
		{
			name: "combine sequential assistant messages",
			messages: []types.Message{
				userText("hi"),
				{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
				{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "World"}}},
			},
			wantMessages: `[{"role":"user","content":[{"type":"text","text":"hi"}]},{"role":"assistant","content":[{"type":"text","text":"Hello"},{"type":"text","text":"World"}]}]`,
		},
		{
			name: "reasoning with signature becomes thinking; without signature warns",
			messages: []types.Message{
				{Role: types.RoleAssistant, Content: []types.ContentPart{
					types.ReasoningContent{Text: "I need to count", ProviderOptions: anthropicOpt("signature", "test-sig")},
					types.ReasoningContent{Text: "no metadata"},
					types.ReasoningContent{ProviderOptions: anthropicOpt("redactedData", "redacted")},
					types.TextContent{Text: "Answer"},
				}},
			},
			wantMessages: `[{"role":"assistant","content":[
				{"type":"thinking","thinking":"I need to count","signature":"test-sig"},
				{"type":"redacted_thinking","data":"redacted"},
				{"type":"text","text":"Answer"}
			]}]`,
			wantWarnings: []string{"unsupported reasoning metadata"},
		},
		{
			name: "omit reasoning when sendReasoning is false",
			opts: AnthropicPromptOptions{SendReasoning: &falseVal},
			messages: []types.Message{
				{Role: types.RoleAssistant, Content: []types.ContentPart{
					types.ReasoningContent{Text: "thinking", Signature: "sig"},
					types.TextContent{Text: "Answer"},
				}},
			},
			wantMessages: `[{"role":"assistant","content":[{"type":"text","text":"Answer"}]}]`,
			wantWarnings: []string{"sending reasoning content is disabled for this model"},
		},
		{
			// TS: "should preserve citations on assistant text"
			name: "preserve citations on assistant text",
			messages: []types.Message{
				{Role: types.RoleAssistant, Content: []types.ContentPart{
					types.TextContent{Text: "The Federal Reserve held rates steady.", ProviderOptions: anthropicOpt("citations", []interface{}{
						map[string]interface{}{
							"type":            "web_search_result_location",
							"cited_text":      "The Committee decided to maintain the rate.",
							"url":             "https://example.com/fed-decision",
							"title":           "Federal Reserve decision",
							"encrypted_index": "encrypted-index",
						},
					})},
				}},
				userText("What happened before that?"),
			},
			wantMessages: `[
				{"role":"assistant","content":[{"type":"text","text":"The Federal Reserve held rates steady.","citations":[
					{"type":"web_search_result_location","cited_text":"The Committee decided to maintain the rate.","url":"https://example.com/fed-decision","title":"Federal Reserve decision","encrypted_index":"encrypted-index"}
				]}]},
				{"role":"user","content":[{"type":"text","text":"What happened before that?"}]}
			]`,
		},
		{
			// TS convert-to-anthropic-prompt.ts drops an assistant message
			// entirely when its converted content ends up empty (e.g. only an
			// empty compaction block), rather than sending an empty content array.
			name: "drop entirely-empty assistant message",
			messages: []types.Message{
				userText("hi"),
				{Role: types.RoleAssistant, Content: []types.ContentPart{
					types.TextContent{Text: "", ProviderOptions: anthropicOpt("type", "compaction")},
				}},
				userText("bye"),
			},
			wantMessages: `[{"role":"user","content":[{"type":"text","text":"hi"}]},{"role":"user","content":[{"type":"text","text":"bye"}]}]`,
		},
		{
			name: "web_search tool call and result",
			messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "srvtoolu_1", ToolName: "web_search", ProviderExecuted: true, Arguments: map[string]interface{}{"query": "SF news"}},
				types.ToolResultContent{ToolCallID: "srvtoolu_1", ToolName: "web_search", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: []interface{}{
					map[string]interface{}{"url": "https://patch.com", "title": "SF Calendar", "pageAge": nil, "encryptedContent": "enc", "type": "web_search_result"},
				}}},
			}}},
			wantMessages: `[{"role":"assistant","content":[
				{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{"query":"SF news"}},
				{"type":"web_search_tool_result","tool_use_id":"srvtoolu_1","content":[
					{"url":"https://patch.com","title":"SF Calendar","page_age":null,"encrypted_content":"enc","type":"web_search_result"}
				]}
			]}]`,
		},
		{
			name: "move regular tool_use after provider-executed web_search results",
			messages: []types.Message{
				{Role: types.RoleAssistant, Content: []types.ContentPart{
					types.TextContent{Text: "I will save a note and search the web."},
					types.ToolCallContent{ToolCallID: "toolu_regular", ToolName: "saveNote", Arguments: map[string]interface{}{"note": "n"}},
					types.ToolCallContent{ToolCallID: "srvtoolu_web_search", ToolName: "web_search", ProviderExecuted: true, Arguments: map[string]interface{}{"query": "q"}},
					types.ToolResultContent{ToolCallID: "srvtoolu_web_search", ToolName: "web_search", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: []interface{}{
						map[string]interface{}{"url": "https://www.nba.com/news", "title": "NBA News", "pageAge": "1 hour ago", "encryptedContent": "enc", "type": "web_search_result"},
					}}},
				}},
				{Role: types.RoleTool, Content: []types.ContentPart{
					types.ToolResultContent{ToolCallID: "toolu_regular", ToolName: "saveNote", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"success": true}}},
				}},
			},
			wantMessages: `[
				{"role":"assistant","content":[
					{"type":"text","text":"I will save a note and search the web."},
					{"type":"server_tool_use","id":"srvtoolu_web_search","name":"web_search","input":{"query":"q"}},
					{"type":"web_search_tool_result","tool_use_id":"srvtoolu_web_search","content":[
						{"url":"https://www.nba.com/news","title":"NBA News","page_age":"1 hour ago","encrypted_content":"enc","type":"web_search_result"}
					]},
					{"type":"tool_use","id":"toolu_regular","name":"saveNote","input":{"note":"n"}}
				]},
				{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_regular","content":"{\"success\":true}"}]}
			]`,
		},
		{
			name: "do not move tool_use across thinking blocks",
			messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ReasoningContent{Text: "t1", Signature: "s1"},
				types.ToolCallContent{ToolCallID: "a", ToolName: "saveNote", Arguments: map[string]interface{}{}},
				types.TextContent{Text: "between"},
				types.ReasoningContent{Text: "t2", Signature: "s2"},
				types.TextContent{Text: "after"},
			}}},
			wantMessages: `[{"role":"assistant","content":[
				{"type":"thinking","thinking":"t1","signature":"s1"},
				{"type":"text","text":"between"},
				{"type":"tool_use","id":"a","name":"saveNote","input":{}},
				{"type":"thinking","thinking":"t2","signature":"s2"},
				{"type":"text","text":"after"}
			]}]`,
		},
		{
			name: "web_search error result (error-json string)",
			messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "s", ToolName: "web_search", ProviderExecuted: true, Arguments: map[string]interface{}{"query": "q"}},
				types.ToolResultContent{ToolCallID: "s", ToolName: "web_search", Output: &types.ToolResultOutput{Type: types.ToolResultOutputErrorJSON, Value: `{"type":"web_search_tool_result_error","errorCode":"max_uses_exceeded"}`}},
			}}},
			wantMessages: `[{"role":"assistant","content":[
				{"type":"server_tool_use","id":"s","name":"web_search","input":{"query":"q"}},
				{"type":"web_search_tool_result","tool_use_id":"s","content":{"type":"web_search_tool_result_error","error_code":"max_uses_exceeded"}}
			]}]`,
		},
		{
			name: "web_fetch tool call and result",
			messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "f", ToolName: "anthropic.web_fetch_20250910", ProviderExecuted: true, Arguments: map[string]interface{}{"url": "https://example.com"}},
				types.ToolResultContent{ToolCallID: "f", ToolName: "anthropic.web_fetch_20250910", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{
					"type": "web_fetch_result", "url": "https://example.com", "retrievedAt": "2025-01-01",
					"content": map[string]interface{}{"type": "document", "title": "Example", "citations": map[string]interface{}{"enabled": true},
						"source": map[string]interface{}{"type": "text", "mediaType": "text/plain", "data": "body"}},
				}}},
			}}},
			wantMessages: `[{"role":"assistant","content":[
				{"type":"server_tool_use","id":"f","name":"web_fetch","input":{"url":"https://example.com"}},
				{"type":"web_fetch_tool_result","tool_use_id":"f","content":{"type":"web_fetch_result","url":"https://example.com","retrieved_at":"2025-01-01",
					"content":{"type":"document","title":"Example","citations":{"enabled":true},"source":{"type":"text","media_type":"text/plain","data":"body"}}}}
			]}]`,
		},
		{
			name: "tool_search_tool_regex call and result",
			messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "ts", ToolName: "tool_search_tool_regex", ProviderExecuted: true, Arguments: map[string]interface{}{"pattern": "weather"}},
				types.ToolResultContent{ToolCallID: "ts", ToolName: "tool_search_tool_regex", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: []interface{}{
					map[string]interface{}{"type": "tool_reference", "toolName": "get_weather"},
				}}},
			}}},
			wantMessages: `[{"role":"assistant","content":[
				{"type":"server_tool_use","id":"ts","name":"tool_search_tool_regex","input":{"pattern":"weather"}},
				{"type":"tool_search_tool_result","tool_use_id":"ts","content":{"type":"tool_search_tool_search_result","tool_references":[{"type":"tool_reference","tool_name":"get_weather"}]}}
			]}]`,
		},
		{
			name: "advisor server_tool_use + advisor_result",
			messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "adv", ToolName: "advisor", ProviderExecuted: true, Arguments: map[string]interface{}{"ignored": true}},
				types.ToolResultContent{ToolCallID: "adv", ToolName: "advisor", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"type": "advisor_result", "text": "advice", "stopReason": "end_turn"}}},
			}}},
			wantMessages: `[{"role":"assistant","content":[
				{"type":"server_tool_use","id":"adv","name":"advisor","input":{}},
				{"type":"advisor_tool_result","tool_use_id":"adv","content":{"type":"advisor_result","text":"advice","stop_reason":"end_turn"}}
			]}]`,
		},
		{
			name: "code_execution 20250825 bash sub-tool strips the discriminator",
			messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "srvtoolu_cache_replay", ToolName: "code_execution", ProviderExecuted: true, Arguments: map[string]interface{}{"type": "bash_code_execution", "command": "echo cache-compatible"}},
				types.ToolResultContent{ToolCallID: "srvtoolu_cache_replay", ToolName: "code_execution", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{
					"type": "bash_code_execution_result", "stdout": "cache-compatible\n", "stderr": "", "return_code": 0, "content": []interface{}{},
				}}},
			}}},
			wantMessages: `[{"role":"assistant","content":[
				{"type":"server_tool_use","id":"srvtoolu_cache_replay","name":"bash_code_execution","input":{"command":"echo cache-compatible"}},
				{"type":"bash_code_execution_tool_result","tool_use_id":"srvtoolu_cache_replay","content":{"type":"bash_code_execution_result","stdout":"cache-compatible\n","stderr":"","return_code":0,"content":[]}}
			]}]`,
		},
		{
			name: "code_execution 20260120 caller metadata and order",
			messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "code-execution-call", ToolName: "code_execution", ProviderExecuted: true,
					Arguments:       map[string]interface{}{"type": "programmatic-tool-call", "code": "await web_search()"},
					ProviderOptions: anthropicOpt("caller", map[string]interface{}{"type": "direct"})},
				types.ToolCallContent{ToolCallID: "ws1", ToolName: "web_search", ProviderExecuted: true, Arguments: map[string]interface{}{"query": "AI SDK"},
					ProviderOptions: anthropicOpt("caller", map[string]interface{}{"type": "code_execution_20260120", "toolId": "code-execution-call"})},
				types.ToolResultContent{ToolCallID: "ws1", ToolName: "web_search", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: []interface{}{}},
					ProviderOptions: anthropicOpt("caller", map[string]interface{}{"type": "code_execution_20260120", "toolId": "code-execution-call"})},
				types.ToolResultContent{ToolCallID: "code-execution-call", ToolName: "code_execution", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{
					"type": "encrypted_code_execution_result", "encrypted_stdout": "enc", "stderr": "", "return_code": 0, "content": []interface{}{},
				}}},
			}}},
			wantMessages: `[{"role":"assistant","content":[
				{"type":"server_tool_use","id":"code-execution-call","name":"code_execution","input":{"code":"await web_search()"},"caller":{"type":"direct"}},
				{"type":"server_tool_use","id":"ws1","name":"web_search","input":{"query":"AI SDK"},"caller":{"type":"code_execution_20260120","tool_id":"code-execution-call"}},
				{"type":"web_search_tool_result","tool_use_id":"ws1","content":[],"caller":{"type":"code_execution_20260120","tool_id":"code-execution-call"}},
				{"type":"code_execution_tool_result","tool_use_id":"code-execution-call","content":{"type":"encrypted_code_execution_result","encrypted_stdout":"enc","stderr":"","return_code":0,"content":[]}}
			]}]`,
		},
		{
			name: "mcp tool use parts",
			messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "mcptoolu_1", ToolName: "echo", ProviderExecuted: true, Arguments: map[string]interface{}{},
					ProviderOptions: anthropicOpt("type", "mcp-tool-use", "serverName", "echo")},
				types.ToolResultContent{ToolCallID: "mcptoolu_1", ToolName: "echo", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: []interface{}{
					map[string]interface{}{"type": "text", "text": "Tool echo: hello world"},
				}}},
				types.TextContent{Text: "done"},
			}}},
			wantMessages: `[{"role":"assistant","content":[
				{"type":"mcp_tool_use","id":"mcptoolu_1","name":"echo","input":{},"server_name":"echo"},
				{"type":"mcp_tool_result","tool_use_id":"mcptoolu_1","is_error":false,"content":[{"type":"text","text":"Tool echo: hello world"}]},
				{"type":"text","text":"done"}
			]}]`,
			wantWarnings: []string{"provider executed tool result for tool echo is not supported"},
		},
		{
			name: "legacy Message.ToolCalls become tool_use blocks (deduplicated against content)",
			messages: []types.Message{
				userText("weather?"),
				{Role: types.RoleAssistant,
					Content: []types.ContentPart{
						types.TextContent{Text: "Let me check."},
						types.ToolCallContent{ToolCallID: "call_1", ToolName: "weather", Arguments: map[string]interface{}{"city": "SF"}},
					},
					ToolCalls: []types.ToolCall{
						{ID: "call_1", ToolName: "weather", Arguments: map[string]interface{}{"city": "SF"}},
						{ID: "call_2", ToolName: "time", RawArguments: `{"tz":"PST"}`},
					}},
			},
			wantMessages: `[
				{"role":"user","content":[{"type":"text","text":"weather?"}]},
				{"role":"assistant","content":[
					{"type":"text","text":"Let me check."},
					{"type":"tool_use","id":"call_1","name":"weather","input":{"city":"SF"}},
					{"type":"tool_use","id":"call_2","name":"time","input":{"tz":"PST"}}
				]}
			]`,
		},
		{
			name: "wrap non-object (invalid) tool call input",
			messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "c", ToolName: "t", Input: `"not an object"`},
			}}},
			wantMessages: `[{"role":"assistant","content":[{"type":"tool_use","id":"c","name":"t","input":{"rawInvalidInput":"not an object"}}]}]`,
		},

		// ── cache control ───────────────────────────────────────────────
		{
			name:         "cache_control on system message",
			messages:     []types.Message{systemText("sys", anthropicOpt("cacheControl", ephemeral))},
			wantMessages: `[]`,
			wantSystem:   `[{"type":"text","text":"sys","cache_control":{"type":"ephemeral"}}]`,
		},
		{
			name: "cache_control on user part and on last part from message",
			messages: []types.Message{{Role: types.RoleUser, ProviderOptions: anthropicOpt("cacheControl", ephemeral), Content: []types.ContentPart{
				types.TextContent{Text: "part1", ProviderOptions: anthropicOpt("cache_control", ephemeral)},
				types.TextContent{Text: "part2"},
				types.TextContent{Text: "part3"},
			}}},
			wantMessages: `[{"role":"user","content":[
				{"type":"text","text":"part1","cache_control":{"type":"ephemeral"}},
				{"type":"text","text":"part2"},
				{"type":"text","text":"part3","cache_control":{"type":"ephemeral"}}
			]}]`,
		},
		{
			name: "cache_control on assistant tool call part",
			messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "c", ToolName: "t", Arguments: map[string]interface{}{"a": 1}, ProviderOptions: anthropicOpt("cacheControl", ephemeral)},
			}}},
			wantMessages: `[{"role":"assistant","content":[{"type":"tool_use","id":"c","name":"t","input":{"a":1},"cache_control":{"type":"ephemeral"}}]}]`,
		},
		{
			name: "cache_control on last tool result from message",
			messages: []types.Message{{Role: types.RoleTool, ProviderOptions: anthropicOpt("cacheControl", ephemeral), Content: []types.ContentPart{
				types.ToolResultContent{ToolCallID: "a", ToolName: "t", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "x"}},
				types.ToolResultContent{ToolCallID: "b", ToolName: "t", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "y"}},
			}}},
			wantMessages: `[{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"a","content":"x"},
				{"type":"tool_result","tool_use_id":"b","content":"y","cache_control":{"type":"ephemeral"}}
			]}]`,
		},
		{
			// Two consecutive tool messages, each with its own message-level
			// cache_control and no part-level cache_control. Core combining
			// (MergeConsecutiveToolMessages, hash 33647d7) merges them into one
			// tool message before Anthropic's own grouping runs. cache_control
			// from the FIRST message must land on ITS OWN last part (not be
			// lost when its message-level ProviderOptions is overwritten by the
			// second message's), and cache_control from the second (now final)
			// message must land on the true last part of the combined result.
			name: "cache_control preserved across combined consecutive tool messages",
			messages: []types.Message{
				{Role: types.RoleTool, ProviderOptions: anthropicOpt("cacheControl", ephemeral), Content: []types.ContentPart{
					types.ToolResultContent{ToolCallID: "a", ToolName: "t", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "x"}},
				}},
				{Role: types.RoleTool, ProviderOptions: anthropicOpt("cacheControl", ephemeral), Content: []types.ContentPart{
					types.ToolResultContent{ToolCallID: "b", ToolName: "t", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "y"}},
				}},
			},
			wantMessages: `[{"role":"user","content":[
				{"type":"tool_result","tool_use_id":"a","content":"x","cache_control":{"type":"ephemeral"}},
				{"type":"tool_result","tool_use_id":"b","content":"y","cache_control":{"type":"ephemeral"}}
			]}]`,
		},
		{
			name: "reject cache_control on thinking blocks",
			messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ReasoningContent{Text: "t", ProviderOptions: anthropicOpt("signature", "s", "cacheControl", ephemeral)},
				types.TextContent{Text: "a"},
			}}},
			wantMessages: `[{"role":"assistant","content":[{"type":"thinking","thinking":"t","signature":"s"},{"type":"text","text":"a"}]}]`,
			wantWarnings: []string{"cache_control cannot be set on thinking block. It will be ignored."},
		},
		{
			name: "limit cache breakpoints to 4",
			messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
				types.TextContent{Text: "1", ProviderOptions: anthropicOpt("cacheControl", ephemeral)},
				types.TextContent{Text: "2", ProviderOptions: anthropicOpt("cacheControl", ephemeral)},
				types.TextContent{Text: "3", ProviderOptions: anthropicOpt("cacheControl", ephemeral)},
				types.TextContent{Text: "4", ProviderOptions: anthropicOpt("cacheControl", ephemeral)},
				types.TextContent{Text: "5", ProviderOptions: anthropicOpt("cacheControl", ephemeral)},
			}}},
			wantMessages: `[{"role":"user","content":[
				{"type":"text","text":"1","cache_control":{"type":"ephemeral"}},
				{"type":"text","text":"2","cache_control":{"type":"ephemeral"}},
				{"type":"text","text":"3","cache_control":{"type":"ephemeral"}},
				{"type":"text","text":"4","cache_control":{"type":"ephemeral"}},
				{"type":"text","text":"5"}
			]}]`,
			wantWarnings: []string{"Maximum 4 cache breakpoints exceeded (found 5). This breakpoint will be ignored."},
		},

		// ── citations ──────────────────────────────────────────────────
		{
			name: "citations, title and context on documents",
			messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
				types.FileContent{Data: []byte("%PDF-1.4"), MediaType: "application/pdf", Filename: "doc.pdf",
					ProviderOptions: anthropicOpt("citations", map[string]interface{}{"enabled": true}, "title", "Custom Title", "context", "Context")},
			}}},
			wantMessages: `[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"JVBERi0xLjQ="},"title":"Custom Title","context":"Context","citations":{"enabled":true}}]}]`,
			wantBetas:    []string{AnthropicBetaPDFs},
		},

		// ── toolsets ───────────────────────────────────────────────────
		{
			name: "toolset tool calls and results with toolset_name",
			opts: AnthropicPromptOptions{ToolsetNames: map[string]string{"computer": "computer"}},
			messages: []types.Message{
				{Role: types.RoleAssistant, Content: []types.ContentPart{
					types.ToolCallContent{ToolCallID: "toolu_click", ToolName: "computer", Arguments: map[string]interface{}{"action": "left_click", "coordinate": []interface{}{640, 60}}},
				}},
				{Role: types.RoleTool, Content: []types.ContentPart{
					types.ToolResultContent{ToolCallID: "toolu_click", ToolName: "computer", Output: &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "OK"}},
				}},
			},
			wantMessages: `[
				{"role":"assistant","content":[{"type":"tool_use","id":"toolu_click","name":"left_click","toolset_name":"computer","input":{"coordinate":[640,60]}}]},
				{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_click","toolset_name":"computer","content":"OK"}]}
			]`,
		},
		{
			name: "warn and skip toolset tool calls without an action",
			opts: AnthropicPromptOptions{ToolsetNames: map[string]string{"computer": "computer"}},
			messages: []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
				types.ToolCallContent{ToolCallID: "toolu_bad", ToolName: "computer", Arguments: map[string]interface{}{"coordinate": []interface{}{1, 2}}},
			}}},
			wantMessages: `[]`,
			wantWarnings: []string{"toolset tool call for tool computer is missing the action"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ConvertToAnthropicPrompt(tt.messages, tt.opts)
			if err != nil {
				t.Fatalf("ConvertToAnthropicPrompt: %v", err)
			}
			assertJSONEqual(t, "messages", got.Messages, tt.wantMessages)
			if tt.wantSystem == "" {
				if got.System != nil {
					t.Errorf("system = %#v, want nil", got.System)
				}
			} else {
				assertJSONEqual(t, "system", got.System, tt.wantSystem)
			}
			wantBetas := tt.wantBetas
			if wantBetas == nil {
				wantBetas = []string{}
			}
			gotBetas := got.Betas
			if gotBetas == nil {
				gotBetas = []string{}
			}
			if !reflect.DeepEqual(gotBetas, wantBetas) {
				t.Errorf("betas = %v, want %v", gotBetas, wantBetas)
			}
			wantWarnings := tt.wantWarnings
			if wantWarnings == nil {
				wantWarnings = []string{}
			}
			if gotWarnings := warningMessages(got.Warnings); !reflect.DeepEqual(gotWarnings, wantWarnings) {
				t.Errorf("warnings = %q, want %q", gotWarnings, wantWarnings)
			}
		})
	}
}

// TS: 'should throw error for unsupported file types'.
func TestConvertToAnthropicPrompt_UnsupportedFileType(t *testing.T) {
	_, err := ConvertToAnthropicPrompt([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{Data: []byte("abc"), MediaType: "video/mp4"},
	}}}, AnthropicPromptOptions{})
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v, want UnsupportedFunctionalityError", err)
	}
}

// TS: 'should throw when provider reference does not contain anthropic key'.
func TestConvertToAnthropicPrompt_MissingAnthropicReference(t *testing.T) {
	_, err := ConvertToAnthropicPrompt([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{FileData: types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"openai": "file-xyz"}}, MediaType: "application/pdf"},
	}}}, AnthropicPromptOptions{})
	if err == nil || !strings.Contains(err.Error(), "anthropic") {
		t.Fatalf("err = %v, want NoSuchProviderReferenceError for anthropic", err)
	}
}

// TS: 'message sequences' — user → assistant(tool calls) → tool → assistant → user.
func TestConvertToAnthropicPrompt_MultiStepToolUseSequence(t *testing.T) {
	messages := []types.Message{
		userText("What is the weather in SF and NYC?"),
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.TextContent{Text: "Let me check both cities."},
			types.ToolCallContent{ToolCallID: "weather-1", ToolName: "weather", Arguments: map[string]interface{}{"city": "SF"}},
			types.ToolCallContent{ToolCallID: "weather-2", ToolName: "weather", Arguments: map[string]interface{}{"city": "NYC"}},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "weather-1", ToolName: "weather", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"temperature": 72}}},
			types.ToolResultContent{ToolCallID: "weather-2", ToolName: "weather", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"temperature": 65}}},
		}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "SF is 72 and NYC is 65."}}},
		userText("Thanks!"),
	}
	got, err := ConvertToAnthropicPrompt(messages, AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, "messages", got.Messages, `[
		{"role":"user","content":[{"type":"text","text":"What is the weather in SF and NYC?"}]},
		{"role":"assistant","content":[
			{"type":"text","text":"Let me check both cities."},
			{"type":"tool_use","id":"weather-1","name":"weather","input":{"city":"SF"}},
			{"type":"tool_use","id":"weather-2","name":"weather","input":{"city":"NYC"}}
		]},
		{"role":"user","content":[
			{"type":"tool_result","tool_use_id":"weather-1","content":"{\"temperature\":72}"},
			{"type":"tool_result","tool_use_id":"weather-2","content":"{\"temperature\":65}"}
		]},
		{"role":"assistant","content":[{"type":"text","text":"SF is 72 and NYC is 65."}]},
		{"role":"user","content":[{"type":"text","text":"Thanks!"}]}
	]`)
}

// Legacy Go tool results (Result / Error fields, no Output) map to the TS
// text / json / error-text outputs.
func TestConvertToAnthropicPrompt_LegacyToolResultFields(t *testing.T) {
	got, err := ConvertToAnthropicPrompt([]types.Message{{Role: types.RoleTool, Content: []types.ContentPart{
		types.ToolResultContent{ToolCallID: "a", ToolName: "t", Result: "plain"},
		types.ToolResultContent{ToolCallID: "b", ToolName: "t", Result: map[string]interface{}{"ok": true}},
		types.ToolResultContent{ToolCallID: "c", ToolName: "t", Error: "failed"},
	}}}, AnthropicPromptOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, "messages", got.Messages, `[{"role":"user","content":[
		{"type":"tool_result","tool_use_id":"a","content":"plain"},
		{"type":"tool_result","tool_use_id":"b","content":"{\"ok\":true}"},
		{"type":"tool_result","tool_use_id":"c","content":"failed","is_error":true}
	]}]`)
}

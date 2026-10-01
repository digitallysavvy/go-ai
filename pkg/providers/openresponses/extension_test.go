package openresponses

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestNewExtensionRegistry_ValidationRules covers row 9a68261 (OR-EXT): the
// registration validation rules mirrored from TS
// createOpenResponsesExtensionRegistry.
func TestNewExtensionRegistry_ValidationRules(t *testing.T) {
	cases := []struct {
		name    string
		ext     Extension
		wantErr bool
	}{
		{
			name:    "missing namespace separator",
			ext:     Extension{ID: "noseparator", EncodeTool: func(string, map[string]interface{}) (map[string]interface{}, error) { return nil, nil }, ToolType: "x:y"},
			wantErr: true,
		},
		{
			name:    "toolType without EncodeTool",
			ext:     Extension{ID: "ns.ext", ToolType: "ns:tool"},
			wantErr: true,
		},
		{
			name:    "EncodeTool without toolType",
			ext:     Extension{ID: "ns.ext", EncodeTool: func(string, map[string]interface{}) (map[string]interface{}, error) { return nil, nil }},
			wantErr: true,
		},
		{
			name: "encodeToolChoice without EncodeTool",
			ext: Extension{
				ID:               "ns.ext",
				ItemTypes:        []string{"ns:item"},
				DecodeItem:       func(ExtensionItem, string) ([]types.ContentPart, error) { return nil, nil },
				EncodeToolChoice: func(string, map[string]interface{}) (map[string]interface{}, error) { return nil, nil },
			},
			wantErr: true,
		},
		{
			name:    "toolType wrong namespace",
			ext:     Extension{ID: "ns.ext", ToolType: "other:tool", EncodeTool: func(string, map[string]interface{}) (map[string]interface{}, error) { return nil, nil }},
			wantErr: true,
		},
		{
			name: "itemTypes wrong namespace",
			ext: Extension{
				ID:         "ns.ext",
				ItemTypes:  []string{"other:item"},
				DecodeItem: func(ExtensionItem, string) ([]types.ContentPart, error) { return nil, nil },
			},
			wantErr: true,
		},
		{
			name:    "no capability registered",
			ext:     Extension{ID: "ns.ext"},
			wantErr: true,
		},
		{
			// Row 9a68261: a non-nil but empty ItemTypes (provided but
			// empty) must get its own "must register at least one item
			// type" error, distinct from the "must provide ItemTypes and
			// DecodeItem together" mismatch error above.
			name: "empty ItemTypes with DecodeItem",
			ext: Extension{
				ID:         "ns.ext",
				ItemTypes:  []string{},
				DecodeItem: func(ExtensionItem, string) ([]types.ContentPart, error) { return nil, nil },
			},
			wantErr: true,
		},
		{
			name: "empty EventTypes with DecodeEvent",
			ext: Extension{
				ID:         "ns.ext",
				EventTypes: []string{},
				DecodeEvent: func(ExtensionEvent, map[string]interface{}) ([]*provider.StreamChunk, error) {
					return nil, nil
				},
			},
			wantErr: true,
		},
		{
			name: "valid full extension",
			ext: Extension{
				ID:         "ns.ext",
				ToolType:   "ns:tool",
				ItemTypes:  []string{"ns:item"},
				EventTypes: []string{"ns:event"},
				EncodeTool: func(string, map[string]interface{}) (map[string]interface{}, error) {
					return map[string]interface{}{}, nil
				},
				DecodeItem: func(ExtensionItem, string) ([]types.ContentPart, error) { return nil, nil },
				DecodeEvent: func(ExtensionEvent, map[string]interface{}) ([]*provider.StreamChunk, error) {
					return nil, nil
				},
			},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewExtensionRegistry([]Extension{tc.ext})
			if (err != nil) != tc.wantErr {
				t.Fatalf("NewExtensionRegistry() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestNewExtensionRegistry_DuplicateRegistration(t *testing.T) {
	makeExt := func(id string) Extension {
		return Extension{
			ID:       id,
			ToolType: "ns:tool",
			EncodeTool: func(string, map[string]interface{}) (map[string]interface{}, error) {
				return map[string]interface{}{}, nil
			},
		}
	}
	_, err := NewExtensionRegistry([]Extension{makeExt("ns.a"), makeExt("ns.a")})
	if err == nil {
		t.Fatal("expected duplicate extension ID to error")
	}

	dupToolType := []Extension{
		{ID: "ns.a", ToolType: "ns:tool", EncodeTool: func(string, map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{}, nil
		}},
		{ID: "ns.b", ToolType: "ns:tool", EncodeTool: func(string, map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{}, nil
		}},
	}
	if _, err := NewExtensionRegistry(dupToolType); err == nil {
		t.Fatal("expected duplicate ToolType to error")
	}
}

// codeExecutionExtension returns a stand-in extension resembling the doc
// comment's example (an "lmstudio.code_execution"-style tool), used across
// the tests below.
func codeExecutionExtension() Extension {
	return Extension{
		ID:       "lmstudio.code_execution",
		ToolType: "lmstudio:code_execution",
		EncodeTool: func(name string, args map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"language": args["language"]}, nil
		},
		ItemTypes: []string{"lmstudio:code_execution"},
		DecodeItem: func(item ExtensionItem, mode string) ([]types.ContentPart, error) {
			code, _ := item["code"].(string)
			result, _ := item["result"].(string)
			return []types.ContentPart{
				types.ToolCallContent{
					ToolCallID:       item.ID(),
					ToolName:         "run_code",
					Input:            code,
					Arguments:        map[string]interface{}{"code": code},
					ProviderExecuted: true,
				},
				types.ToolResultContent{
					ToolCallID:       item.ID(),
					ToolName:         "run_code",
					Result:           result,
					ProviderExecuted: true,
				},
			}, nil
		},
		EncodeInputItem: func(part types.ContentPart, tool types.Tool) ([]ExtensionItem, error) {
			tc, ok := part.(types.ToolCallContent)
			if !ok {
				return nil, nil
			}
			return []ExtensionItem{{
				"type":   "lmstudio:code_execution",
				"id":     tc.ToolCallID,
				"status": "completed",
				"code":   tc.Input,
			}}, nil
		},
	}
}

// TestOpenResponsesExtensionToolPrepareAndChoice covers row 9a68261: a
// registered extension's provider-defined tool encodes into the request
// body's tools array, and tool_choice targeting it is encoded via the
// extension's ToolType.
func TestOpenResponsesExtensionToolPrepareAndChoice(t *testing.T) {
	registry, err := NewExtensionRegistry([]Extension{codeExecutionExtension()})
	if err != nil {
		t.Fatalf("NewExtensionRegistry failed: %v", err)
	}
	p := New(Config{BaseURL: "http://localhost:1234/v1", Extensions: []Extension{codeExecutionExtension()}})
	_ = registry
	model := NewLanguageModel(p, "local-model")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools: []types.Tool{
			{Name: "run_code", Type: "provider", ProviderID: "lmstudio.code_execution", ProviderArgs: map[string]interface{}{"language": "python"}},
		},
		ToolChoice: types.ToolChoice{Type: "tool", ToolName: "run_code"},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	tools, ok := body["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want one encoded extension tool", body["tools"])
	}
	toolDef, ok := tools[0].(map[string]interface{})
	if !ok || toolDef["type"] != "lmstudio:code_execution" || toolDef["language"] != "python" {
		t.Fatalf("tool def = %#v, want lmstudio:code_execution with language=python", tools[0])
	}
	choice, ok := body["tool_choice"].(map[string]interface{})
	if !ok || choice["type"] != "lmstudio:code_execution" {
		t.Fatalf("tool_choice = %#v, want {type: lmstudio:code_execution}", body["tool_choice"])
	}
}

// TestOpenResponsesExtensionToolChoiceUsesToolArgs covers row 9a68261: TS
// calls encodeToolChoice({name: tool.name, args: tool.args}) using the
// provider tool's own declared args, not always nil.
func TestOpenResponsesExtensionToolChoiceUsesToolArgs(t *testing.T) {
	ext := Extension{
		ID:       "lmstudio.code_execution",
		ToolType: "lmstudio:code_execution",
		EncodeTool: func(name string, args map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"language": args["language"]}, nil
		},
		EncodeToolChoice: func(name string, args map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{"language": args["language"]}, nil
		},
	}
	p := New(Config{BaseURL: "http://localhost:1234/v1", Extensions: []Extension{ext}})
	model := NewLanguageModel(p, "local-model")

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools: []types.Tool{
			{Name: "run_code", Type: "provider", ProviderID: "lmstudio.code_execution", ProviderArgs: map[string]interface{}{"language": "python"}},
		},
		ToolChoice: types.ToolChoice{Type: "tool", ToolName: "run_code"},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	choice, ok := body["tool_choice"].(map[string]interface{})
	if !ok || choice["language"] != "python" {
		t.Fatalf("tool_choice = %#v, want language=python threaded from the tool's ProviderArgs", body["tool_choice"])
	}
}

// TestOpenResponsesExtensionToolChoiceEncodeFailureWarns covers row 9a68261:
// when EncodeToolChoice is set but fails/returns invalid output, TS emits an
// "unsupported: tool choice for provider-defined tool <id>" warning and
// omits tool_choice entirely (not a {"type": toolType} fallback).
func TestOpenResponsesExtensionToolChoiceEncodeFailureWarns(t *testing.T) {
	ext := Extension{
		ID:       "lmstudio.code_execution",
		ToolType: "lmstudio:code_execution",
		EncodeTool: func(name string, args map[string]interface{}) (map[string]interface{}, error) {
			return map[string]interface{}{}, nil
		},
		EncodeToolChoice: func(name string, args map[string]interface{}) (map[string]interface{}, error) {
			return nil, nil
		},
	}
	p := New(Config{BaseURL: "http://localhost:1234/v1", Extensions: []Extension{ext}})
	model := NewLanguageModel(p, "local-model")

	body, warnings, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools: []types.Tool{
			{Name: "run_code", Type: "provider", ProviderID: "lmstudio.code_execution"},
		},
		ToolChoice: types.ToolChoice{Type: "tool", ToolName: "run_code"},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	if _, ok := body["tool_choice"]; ok {
		t.Fatalf("tool_choice = %#v, want omitted after an EncodeToolChoice failure", body["tool_choice"])
	}
	assertUnsupportedWarning(t, warnings, "tool choice for provider-defined tool lmstudio.code_execution")
}

// TestOpenResponsesExtensionToolUnsupportedWithoutRegistry covers row
// 9a68261: without a matching extension, a provider-defined tool is still
// skipped with an "unsupported" warning (pre-existing OR-CORE behavior).
func TestOpenResponsesExtensionToolUnsupportedWithoutRegistry(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1"})
	model := NewLanguageModel(p, "local-model")

	body, warnings, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools: []types.Tool{
			{Name: "run_code", Type: "provider", ProviderID: "lmstudio.code_execution"},
		},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	if _, ok := body["tools"]; ok {
		t.Fatalf("tools = %#v, want omitted when no tools could be encoded", body["tools"])
	}
	assertUnsupportedWarning(t, warnings, "provider-defined tool lmstudio.code_execution")
}

// TestOpenResponsesExtensionItemDecodeGenerate covers row 9a68261: an
// unrecognized output item matching a registered extension's ItemTypes
// decodes into a provider-executed tool call/result pair, driving the
// tool-calls finish reason, with a replay carrier preserved for round-trip.
func TestOpenResponsesExtensionItemDecodeGenerate(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1", Extensions: []Extension{codeExecutionExtension()}})
	model := NewLanguageModel(p, "local-model")

	raw := json.RawMessage(`{"type":"lmstudio:code_execution","id":"ce_1","status":"completed","code":"print(1)","result":"1"}`)
	var item OutputItem
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatalf("unmarshal item failed: %v", err)
	}
	result, convertErr := model.convertResponse(OpenResponsesResponse{Output: []OutputItem{item}})
	if convertErr != nil {
		t.Fatalf("convertResponse failed: %v", convertErr)
	}

	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ToolName != "run_code" || !result.ToolCalls[0].ProviderExecuted {
		t.Fatalf("ToolCalls = %#v, want one provider-executed run_code call", result.ToolCalls)
	}
	if result.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("FinishReason = %v, want tool-calls", result.FinishReason)
	}

	var sawCarrier, sawResult bool
	for _, part := range result.Content {
		switch c := part.(type) {
		case types.CustomContent:
			if c.Kind == extensionReplayKind {
				sawCarrier = true
			}
		case types.ToolResultContent:
			if c.Result == "1" {
				sawResult = true
			}
		}
	}
	if !sawCarrier {
		t.Fatalf("result.Content = %#v, want an extension replay carrier", result.Content)
	}
	if !sawResult {
		t.Fatalf("result.Content = %#v, want the decoded tool result", result.Content)
	}
}

// TestOpenResponsesExtensionItemDecodeGenerateErrorAborts covers P1-5c item
// 6: when a registered extension's DecodeItem fails on a known extension
// item type, convertResponse (and therefore DoGenerate) aborts the whole
// call with that error instead of silently skipping the malformed item --
// matching TS doGenerate, which has no try/catch around decodeExtensionItem.
func TestOpenResponsesExtensionItemDecodeGenerateErrorAborts(t *testing.T) {
	failingExt := Extension{
		ID:        "lmstudio.broken",
		ItemTypes: []string{"lmstudio:broken"},
		DecodeItem: func(item ExtensionItem, mode string) ([]types.ContentPart, error) {
			return nil, errors.New("boom")
		},
	}
	p := New(Config{BaseURL: "http://localhost:1234/v1", Extensions: []Extension{failingExt}})
	model := NewLanguageModel(p, "local-model")

	raw := json.RawMessage(`{"type":"lmstudio:broken","id":"b_1","status":"completed"}`)
	var item OutputItem
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatalf("unmarshal item failed: %v", err)
	}
	result, convertErr := model.convertResponse(OpenResponsesResponse{Output: []OutputItem{item}})
	if convertErr == nil {
		t.Fatalf("convertResponse() = %#v, nil, want an error from the failing DecodeItem", result)
	}
	if !strings.Contains(convertErr.Error(), "boom") {
		t.Fatalf("convertResponse() error = %v, want it to wrap the DecodeItem error", convertErr)
	}
}

// TestOpenResponsesExtensionItemReplayCarrier covers row 9a68261: an
// extension-decoded tool call/result round-trips through input conversion
// by resending the original wire item verbatim via its replay carrier.
func TestOpenResponsesExtensionItemReplayCarrier(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1", Extensions: []Extension{codeExecutionExtension()}})
	model := NewLanguageModel(p, "local-model")

	raw := json.RawMessage(`{"type":"lmstudio:code_execution","id":"ce_1","status":"completed","code":"print(1)","result":"1"}`)
	var item OutputItem
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatalf("unmarshal item failed: %v", err)
	}
	result, convertErr := model.convertResponse(OpenResponsesResponse{Output: []OutputItem{item}})
	if convertErr != nil {
		t.Fatalf("convertResponse failed: %v", convertErr)
	}

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleAssistant, Content: result.Content},
		}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	input, ok := body["input"].([]interface{})
	if !ok {
		t.Fatalf("input = %#v, want an input item array", body["input"])
	}
	var found bool
	for _, in := range input {
		m, ok := in.(map[string]interface{})
		if ok && m["type"] == "lmstudio:code_execution" && m["id"] == "ce_1" && m["code"] == "print(1)" {
			found = true
		}
	}
	if !found {
		t.Fatalf("input = %#v, want the original lmstudio:code_execution item resent verbatim", input)
	}
}

// TestOpenResponsesExtensionItemEncodeInputItemFallback covers row 9a68261:
// a fresh extension tool call (no preserved raw item) replays via the
// extension's EncodeInputItem.
func TestOpenResponsesExtensionItemEncodeInputItemFallback(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1", Extensions: []Extension{codeExecutionExtension()}})
	model := NewLanguageModel(p, "local-model")

	refMeta, _ := json.Marshal(map[string]interface{}{
		"open-responses": map[string]interface{}{
			"openResponsesExtension": map[string]interface{}{"id": "lmstudio.code_execution", "itemId": "ce_fresh"},
		},
	})
	toolCall := types.ToolCallContent{
		ToolCallID:       "ce_fresh",
		ToolName:         "run_code",
		Input:            "print(2)",
		ProviderExecuted: true,
		ProviderMetadata: refMeta,
	}

	body, _, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleAssistant, Content: []types.ContentPart{toolCall}},
		}},
		Tools: []types.Tool{{Name: "run_code", Type: "provider", ProviderID: "lmstudio.code_execution"}},
	}, false)
	if err != nil {
		t.Fatalf("buildRequestBody failed: %v", err)
	}
	input, ok := body["input"].([]interface{})
	if !ok || len(input) != 1 {
		t.Fatalf("input = %#v, want one EncodeInputItem-produced item", body["input"])
	}
	m, ok := input[0].(map[string]interface{})
	if !ok || m["type"] != "lmstudio:code_execution" || m["code"] != "print(2)" {
		t.Fatalf("input[0] = %#v, want the EncodeInputItem-encoded item", input[0])
	}
}

// TestOpenResponsesExtensionItemDecodeStream covers row 9a68261: an
// unrecognized item type in a response.output_item.done event matching a
// registered extension decodes into tool-call/tool-result stream chunks.
func TestOpenResponsesExtensionItemDecodeStream(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1", Extensions: []Extension{codeExecutionExtension()}})

	var item OutputItem
	raw := json.RawMessage(`{"type":"lmstudio:code_execution","id":"ce_1","status":"completed","code":"print(1)","result":"1"}`)
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatalf("unmarshal item failed: %v", err)
	}

	s := newOpenResponsesStream(io.NopCloser(strings.NewReader("")), nil, "open-responses")
	s.extensionRegistry = p.extensionRegistry

	// handleStreamEvent queues the decoded parts onto s.pending and reports
	// ok=false (meaning "continue the loop"); draining s.pending is Next()'s
	// job, so read the carrier chunk back through Next() as a real caller
	// would, rather than through the no-longer-recursive internal call.
	if _, ok := s.handleStreamEvent(&StreamEvent{Type: "response.output_item.done", Item: &item}); ok {
		t.Fatalf("handleStreamEvent returned a chunk directly, want it queued via s.pending")
	}
	carrierChunk, err := s.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if carrierChunk.Type != provider.ChunkTypeCustom || carrierChunk.CustomContent == nil || carrierChunk.CustomContent.Kind != extensionReplayKind {
		t.Fatalf("carrierChunk = %#v, want the extension replay carrier first", carrierChunk)
	}
	if !s.hasToolCalls {
		t.Fatal("hasToolCalls = false, want true after decoding an extension tool call")
	}

	toolCallChunk, err := s.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if toolCallChunk.Type != provider.ChunkTypeToolCall || toolCallChunk.ToolCall.ToolName != "run_code" {
		t.Fatalf("toolCallChunk = %#v, want a run_code tool call", toolCallChunk)
	}

	toolResultChunk, err := s.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if toolResultChunk.Type != provider.ChunkTypeToolResult || toolResultChunk.ToolResult.ToolCallID != "ce_1" {
		t.Fatalf("toolResultChunk = %#v, want the queued tool-result chunk", toolResultChunk)
	}
}

// citationExtension is a test extension whose DecodeItem returns a
// types.SourceContent part -- one of the OpenResponsesExtensionContentPart
// union members besides tool-call/tool-result (TS's
// Extract<LanguageModelV4Content, LanguageModelV4StreamPart> covers
// tool-call, tool-result, custom, file, reasoning-file,
// tool-approval-request, and source).
func citationExtension() Extension {
	return Extension{
		ID:        "lmstudio.citation",
		ItemTypes: []string{"lmstudio:citation"},
		DecodeItem: func(item ExtensionItem, mode string) ([]types.ContentPart, error) {
			url, _ := item["url"].(string)
			return []types.ContentPart{
				types.SourceContent{SourceType: "url", ID: item.ID(), URL: url},
			}, nil
		},
	}
}

// TestOpenResponsesExtensionSourceContentGenerate covers row 9a68261: a
// decodeItem that returns a non-tool-call/tool-result content type (source)
// is still included in result.Content for a non-streaming response, exactly
// like any other decoded part.
func TestOpenResponsesExtensionSourceContentGenerate(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1", Extensions: []Extension{citationExtension()}})
	model := NewLanguageModel(p, "local-model")

	raw := json.RawMessage(`{"type":"lmstudio:citation","id":"cit_1","status":"completed","url":"https://example.com"}`)
	var item OutputItem
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatalf("unmarshal item failed: %v", err)
	}
	result, convertErr := model.convertResponse(OpenResponsesResponse{Output: []OutputItem{item}})
	if convertErr != nil {
		t.Fatalf("convertResponse failed: %v", convertErr)
	}

	var sawSource bool
	for _, part := range result.Content {
		if sc, ok := part.(types.SourceContent); ok && sc.URL == "https://example.com" {
			sawSource = true
		}
	}
	if !sawSource {
		t.Fatalf("result.Content = %#v, want the decoded source content", result.Content)
	}
}

// TestOpenResponsesExtensionSourceContentStream covers row 9a68261: TS
// forwards every part an extension's decodeItem returns unconditionally
// (controller.enqueue(part) for each decoded part), including content types
// other than tool-call/tool-result/custom. The streaming decode path must
// not silently drop them.
func TestOpenResponsesExtensionSourceContentStream(t *testing.T) {
	p := New(Config{BaseURL: "http://localhost:1234/v1", Extensions: []Extension{citationExtension()}})

	raw := json.RawMessage(`{"type":"lmstudio:citation","id":"cit_1","status":"completed","url":"https://example.com"}`)
	var item OutputItem
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatalf("unmarshal item failed: %v", err)
	}

	s := newOpenResponsesStream(io.NopCloser(strings.NewReader("")), nil, "open-responses")
	s.extensionRegistry = p.extensionRegistry

	// handleStreamEvent queues the decoded parts onto s.pending and reports
	// ok=false (meaning "continue the loop"); draining s.pending is Next()'s
	// job, so read the carrier chunk back through Next() as a real caller
	// would, rather than through the no-longer-recursive internal call.
	if _, ok := s.handleStreamEvent(&StreamEvent{Type: "response.output_item.done", Item: &item}); ok {
		t.Fatalf("handleStreamEvent returned a chunk directly, want it queued via s.pending")
	}
	carrierChunk, err := s.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if carrierChunk.Type != provider.ChunkTypeCustom || carrierChunk.CustomContent == nil || carrierChunk.CustomContent.Kind != extensionReplayKind {
		t.Fatalf("carrierChunk = %#v, want the extension replay carrier first", carrierChunk)
	}

	chunk, err := s.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if chunk.Type != provider.ChunkTypeSource || chunk.SourceContent == nil || chunk.SourceContent.URL != "https://example.com" {
		t.Fatalf("chunk = %#v, want a forwarded source chunk", chunk)
	}
}

// TestOpenResponsesExtensionEventDecodeStream covers row 9a68261: a
// namespaced streaming event matching a registered extension's EventTypes
// decodes into stream chunks via DecodeEvent, using the stream's persistent
// state map across calls.
func TestOpenResponsesExtensionEventDecodeStream(t *testing.T) {
	ext := Extension{
		ID:         "lmstudio.progress",
		EventTypes: []string{"lmstudio:progress"},
		DecodeEvent: func(event ExtensionEvent, state map[string]interface{}) ([]*provider.StreamChunk, error) {
			percent, _ := event["percent"].(float64)
			calls, _ := state["calls"].(int)
			state["calls"] = calls + 1
			return []*provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "progress"},
				{Type: provider.ChunkTypeReasoning, Reasoning: strconv.Itoa(int(percent))},
			}, nil
		},
	}
	p := New(Config{BaseURL: "http://localhost:1234/v1", Extensions: []Extension{ext}})

	body := `data: {"type":"lmstudio:progress","sequence_number":1,"percent":50}

data: {"type":"response.completed","response":{"id":"r1","output":[],"status":"completed"}}

`
	s := newOpenResponsesStream(io.NopCloser(strings.NewReader(body)), nil, "open-responses")
	s.extensionRegistry = p.extensionRegistry

	chunk1, err := s.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if chunk1.Type != provider.ChunkTypeText || chunk1.Text != "progress" {
		t.Fatalf("chunk1 = %#v, want a text chunk from DecodeEvent", chunk1)
	}
	chunk2, err := s.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if chunk2.Type != provider.ChunkTypeReasoning || chunk2.Reasoning != "50" {
		t.Fatalf("chunk2 = %#v, want a reasoning chunk carrying the percent", chunk2)
	}
	if s.extensionState["calls"] != 1 {
		t.Fatalf("extensionState[calls] = %#v, want 1 (state persists across the stream)", s.extensionState["calls"])
	}
}

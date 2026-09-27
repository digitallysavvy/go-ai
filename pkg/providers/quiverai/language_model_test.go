package quiverai

// Ported from packages/quiverai/src/quiverai-language-model.test.ts
// (ai@7.0.118). Go adaptations, documented inline where they diverge:
//   - No literal WORKFLOW_SERIALIZE/WORKFLOW_DESERIALIZE round-trip through
//     JSON.parse(JSON.stringify(...)); Go's equivalent is
//     provider.SerializeModel/DeserializeModel, exercised in
//     TestLanguageModelWorkflowSerialization.
//   - Go's provider.SerializableConfig intentionally excludes APIKey from
//     serialized config (a deliberate Go SDK security decision predating
//     this slice, see pkg/providers/openresponses/serialization_test.go) --
//     unlike TS's serializeModelOptions, which resolves and serializes the
//     computed Authorization header. The workflow test below asserts this
//     Go-specific exclusion instead of literally re-asserting the same
//     Authorization header after round-trip.
//   - openresponses' non-streaming text conversion does not currently
//     attach per-text-block providerMetadata (itemId); assertions below
//     check result.Text instead of a content-part's providerMetadata for
//     text. This is a pre-existing openresponses gap unrelated to this
//     slice (see final report).

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func reasoningPtr(v types.ReasoningLevel) *types.ReasoningLevel { return &v }

func quiverPrompt(text string) types.Prompt {
	return types.Prompt{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: text}}},
	}}
}

// quiverResponse builds a minimal Open Responses response body, mirroring
// the TS test's createResponse helper.
func quiverResponse(output []map[string]interface{}, status string, extra map[string]interface{}) map[string]interface{} {
	if status == "" {
		status = "completed"
	}
	if output == nil {
		// A nil Go slice marshals to JSON null, which openresponses treats
		// as "Responses API returned no output"; an empty (non-nil) slice
		// marshals to [], matching the TS test fixtures' `output: []`.
		output = []map[string]interface{}{}
	}
	body := map[string]interface{}{
		"id":         "resp_1",
		"object":     "response",
		"created_at": 1700000000,
		"model":      "arrow-2",
		"status":     status,
		"output":     output,
		"usage": map[string]interface{}{
			"input_tokens":         20,
			"input_tokens_details": map[string]interface{}{"cached_tokens": 5},
			"output_tokens":        12,
			"output_tokens_details": map[string]interface{}{
				"reasoning_tokens": 4,
			},
			"total_tokens": 32,
		},
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

func newQuiverTestServer(t *testing.T, handler http.HandlerFunc) (*Provider, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	// LanguageModel appends "/responses" to BaseURL to match createQuiverAI's
	// `${baseURL}/responses` endpoint; point BaseURL at the server root so
	// the model's request lands on "/responses".
	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	return p, server
}

func TestLanguageModelGeneratesText(t *testing.T) {
	var requestBody map[string]interface{}
	var requestPath string
	var authHeader, userAgent string
	p, _ := newQuiverTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		authHeader = r.Header.Get("Authorization")
		userAgent = r.Header.Get("User-Agent")
		_ = json.NewDecoder(r.Body).Decode(&requestBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(quiverResponse([]map[string]interface{}{
			{
				"id": "msg_1", "type": "message", "role": "assistant", "status": "completed",
				"content": []map[string]interface{}{
					{"type": "output_text", "text": "Use a simple blue compass.", "annotations": []interface{}{}},
				},
			},
		}, "", nil))
	})

	model, err := p.LanguageModel(ModelArrow2)
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: quiverPrompt("Create an icon."),
		ProviderOptions: map[string]interface{}{
			"quiverai": map[string]interface{}{
				"reasoningEffort":  "high",
				"reasoningSummary": "auto",
			},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if result.Text != "Use a simple blue compass." {
		t.Fatalf("Text = %q", result.Text)
	}
	if result.FinishReason != types.FinishReasonStop {
		t.Fatalf("FinishReason = %q", result.FinishReason)
	}
	if result.Usage.InputTokens == nil || *result.Usage.InputTokens != 20 {
		t.Fatalf("InputTokens = %+v", result.Usage.InputTokens)
	}
	if result.Usage.InputDetails == nil || result.Usage.InputDetails.CacheReadTokens == nil || *result.Usage.InputDetails.CacheReadTokens != 5 {
		t.Fatalf("InputDetails = %+v", result.Usage.InputDetails)
	}
	if result.Usage.OutputDetails == nil || result.Usage.OutputDetails.ReasoningTokens == nil || *result.Usage.OutputDetails.ReasoningTokens != 4 {
		t.Fatalf("OutputDetails = %+v", result.Usage.OutputDetails)
	}

	if requestPath != "/responses" {
		t.Fatalf("requestPath = %q", requestPath)
	}
	if authHeader != "Bearer test-api-key" {
		t.Fatalf("Authorization = %q", authHeader)
	}
	if !strings.Contains(userAgent, "go-ai/quiverai/") {
		t.Fatalf("User-Agent = %q, want go-ai/quiverai/ substring", userAgent)
	}
	if requestBody["model"] != ModelArrow2 {
		t.Fatalf("model = %v", requestBody["model"])
	}
	reasoning, _ := requestBody["reasoning"].(map[string]interface{})
	if reasoning["effort"] != "high" || reasoning["summary"] != "auto" {
		t.Fatalf("reasoning = %+v", reasoning)
	}
	if _, hasText := requestBody["text"]; hasText {
		t.Fatalf("text field should be omitted, got %v", requestBody["text"])
	}
	input, _ := requestBody["input"].([]interface{})
	if len(input) != 1 {
		t.Fatalf("input = %+v", input)
	}
	item, _ := input[0].(map[string]interface{})
	if item["type"] != "message" || item["role"] != "user" {
		t.Fatalf("input item = %+v", item)
	}
}

func TestLanguageModelValidatesReasoningValues(t *testing.T) {
	p, _ := newQuiverTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request should not be sent for invalid reasoning options")
	})
	model, err := p.LanguageModel(ModelArrow2)
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	for _, method := range []string{"DoGenerate", "DoStream"} {
		for _, opts := range []map[string]interface{}{
			{"reasoningEffort": "max"},
			{"reasoningEffort": "none"},
			{"reasoningSummary": "detailed"},
		} {
			genOpts := &provider.GenerateOptions{
				Prompt:          quiverPrompt("Create an icon."),
				ProviderOptions: map[string]interface{}{"quiverai": opts},
			}
			var callErr error
			if method == "DoGenerate" {
				_, callErr = model.DoGenerate(context.Background(), genOpts)
			} else {
				_, callErr = model.DoStream(context.Background(), genOpts)
			}
			if !providererrors.IsInvalidArgumentError(callErr) {
				t.Fatalf("%s(%v): expected InvalidArgumentError, got %v", method, opts, callErr)
			}
		}
	}
}

func TestLanguageModelWarnsAndOmitsStructuredOutput(t *testing.T) {
	var requestBody map[string]interface{}
	p, _ := newQuiverTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&requestBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(quiverResponse(nil, "", nil))
	})
	model, err := p.LanguageModel(ModelArrow2)
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:         quiverPrompt("Create an icon."),
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: map[string]interface{}{"type": "object"}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	found := false
	for _, w := range result.Warnings {
		if w.Type == "unsupported" && w.Feature == "responseFormat" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %+v, want responseFormat unsupported warning", result.Warnings)
	}
	if _, hasText := requestBody["text"]; hasText {
		t.Fatalf("text field should be omitted when structured outputs are disabled, got %v", requestBody["text"])
	}
}

func TestLanguageModelOmitsReasoningNoneAndWarns(t *testing.T) {
	for _, method := range []string{"DoGenerate", "DoStream"} {
		t.Run(method, func(t *testing.T) {
			var requestBody map[string]interface{}
			p, _ := newQuiverTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&requestBody)
				if method == "DoGenerate" {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(quiverResponse(nil, "", nil))
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
			})
			model, err := p.LanguageModel(ModelArrow2)
			if err != nil {
				t.Fatalf("LanguageModel: %v", err)
			}

			genOpts := &provider.GenerateOptions{
				Prompt:    quiverPrompt("Create an icon."),
				Reasoning: reasoningPtr(types.ReasoningNone),
			}

			var warnings []types.Warning
			if method == "DoGenerate" {
				result, err := model.DoGenerate(context.Background(), genOpts)
				if err != nil {
					t.Fatalf("DoGenerate: %v", err)
				}
				warnings = result.Warnings
			} else {
				stream, err := model.DoStream(context.Background(), genOpts)
				if err != nil {
					t.Fatalf("DoStream: %v", err)
				}
				defer stream.Close()
				chunk, err := stream.Next()
				if err != nil {
					t.Fatalf("Next: %v", err)
				}
				if chunk.Type != provider.ChunkTypeStreamStart {
					t.Fatalf("first chunk = %+v, want stream-start", chunk)
				}
				warnings = chunk.Warnings
			}

			found := false
			for _, w := range warnings {
				if w.Type == "unsupported" && w.Feature == "reasoning effort none" {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s warnings = %+v, want reasoning-effort-none warning", method, warnings)
			}
			if _, hasReasoning := requestBody["reasoning"]; hasReasoning {
				t.Fatalf("%s: reasoning field should be omitted, got %v", method, requestBody["reasoning"])
			}
		})
	}
}

func TestLanguageModelReplaysOpaqueReasoningIDs(t *testing.T) {
	for _, method := range []string{"DoGenerate", "DoStream"} {
		t.Run(method, func(t *testing.T) {
			var requestBody map[string]interface{}
			p, _ := newQuiverTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&requestBody)
				if method == "DoGenerate" {
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(quiverResponse(nil, "", nil))
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
			})
			model, err := p.LanguageModel(ModelArrow2)
			if err != nil {
				t.Fatalf("LanguageModel: %v", err)
			}

			prompt := types.Prompt{Messages: []types.Message{
				{
					Role: types.RoleAssistant,
					Content: []types.ContentPart{
						types.ReasoningContent{
							Text: "private reasoning",
							ProviderOptions: map[string]interface{}{
								"quiverai": map[string]interface{}{
									"itemId": "rs_opaque",
									"reasoningSummary": []interface{}{
										map[string]interface{}{"type": "summary_text", "text": "safe summary"},
									},
									"reasoningContent": []interface{}{
										map[string]interface{}{"type": "reasoning_text", "text": "private reasoning"},
									},
									"reasoningEncryptedContent": "encrypted",
								},
							},
						},
						types.ToolCallContent{
							ToolCallID:      "call_1",
							ToolName:        "write_file",
							Input:           `{"path":"icon.svg"}`,
							ProviderOptions: map[string]interface{}{"quiverai": map[string]interface{}{"itemId": "fc_1"}},
						},
					},
				},
				{
					Role: types.RoleTool,
					Content: []types.ContentPart{
						types.ToolResultContent{
							ToolCallID: "call_1",
							ToolName:   "write_file",
							Output:     &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"staged": true}},
						},
					},
				},
			}}

			genOpts := &provider.GenerateOptions{Prompt: prompt}
			if method == "DoGenerate" {
				if _, err := model.DoGenerate(context.Background(), genOpts); err != nil {
					t.Fatalf("DoGenerate: %v", err)
				}
			} else {
				stream, err := model.DoStream(context.Background(), genOpts)
				if err != nil {
					t.Fatalf("DoStream: %v", err)
				}
				drainStream(t, stream)
			}

			input, _ := requestBody["input"].([]interface{})
			if len(input) != 3 {
				t.Fatalf("input = %+v, want 3 items", input)
			}
			reasoningItem, _ := input[0].(map[string]interface{})
			if reasoningItem["type"] != "reasoning" || reasoningItem["id"] != "rs_opaque" {
				t.Fatalf("reasoning item = %+v", reasoningItem)
			}
			if _, hasContent := reasoningItem["content"]; hasContent {
				t.Fatalf("reasoning item should have no content, got %v", reasoningItem["content"])
			}
			if _, hasEncrypted := reasoningItem["encrypted_content"]; hasEncrypted {
				t.Fatalf("reasoning item should have no encrypted_content, got %v", reasoningItem["encrypted_content"])
			}
			summary, _ := reasoningItem["summary"].([]interface{})
			if len(summary) != 1 {
				t.Fatalf("reasoning summary = %+v", summary)
			}

			functionCall, _ := input[1].(map[string]interface{})
			if functionCall["type"] != "function_call" || functionCall["id"] != "fc_1" || functionCall["call_id"] != "call_1" || functionCall["name"] != "write_file" {
				t.Fatalf("function_call item = %+v", functionCall)
			}
			if functionCall["arguments"] != `{"path":"icon.svg"}` {
				t.Fatalf("function_call arguments = %v", functionCall["arguments"])
			}

			functionOutput, _ := input[2].(map[string]interface{})
			if functionOutput["type"] != "function_call_output" || functionOutput["call_id"] != "call_1" {
				t.Fatalf("function_call_output item = %+v", functionOutput)
			}
			if functionOutput["output"] != `{"staged":true}` {
				t.Fatalf("function_call_output.output = %v", functionOutput["output"])
			}
		})
	}
}

func TestLanguageModelSupportsCustomTools(t *testing.T) {
	var requestBody map[string]interface{}
	p, _ := newQuiverTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&requestBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(quiverResponse([]map[string]interface{}{
			{
				"id": "ct_1", "type": "custom_tool_call", "status": "completed",
				"call_id": "call_custom_1", "name": "write_svg", "input": "<svg />",
			},
		}, "", nil))
	})
	model, err := p.LanguageModel(ModelArrow2)
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: quiverPrompt("Create an icon."),
		Tools: []types.Tool{
			{
				Type:       "provider",
				ProviderID: "quiverai.custom",
				Name:       "write_svg",
				ProviderArgs: map[string]interface{}{
					"description": "Return SVG source.",
					"format":      map[string]interface{}{"type": "text"},
				},
			},
		},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "write_svg"},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}

	tools, _ := requestBody["tools"].([]interface{})
	if len(tools) != 1 {
		t.Fatalf("tools = %+v", tools)
	}
	tool, _ := tools[0].(map[string]interface{})
	if tool["type"] != "custom" || tool["name"] != "write_svg" || tool["description"] != "Return SVG source." {
		t.Fatalf("tool = %+v", tool)
	}
	format, _ := tool["format"].(map[string]interface{})
	if format["type"] != "text" {
		t.Fatalf("tool format = %+v", format)
	}
	toolChoice, _ := requestBody["tool_choice"].(map[string]interface{})
	if toolChoice["type"] != "custom" || toolChoice["name"] != "write_svg" {
		t.Fatalf("tool_choice = %+v", toolChoice)
	}

	if len(result.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %+v", result.ToolCalls)
	}
	call := result.ToolCalls[0]
	if call.ID != "call_custom_1" || call.ToolName != "write_svg" {
		t.Fatalf("tool call = %+v", call)
	}
	assertRawArgumentsDecodeTo(t, call.RawArguments, "<svg />")
	if call.ProviderMetadata["quiverai"] == nil {
		t.Fatalf("ProviderMetadata missing quiverai key: %+v", call.ProviderMetadata)
	}
}

func TestLanguageModelStreamsFunctionCalls(t *testing.T) {
	completed := quiverResponse([]map[string]interface{}{
		{"id": "fc_1", "type": "function_call", "status": "completed", "call_id": "call_1", "name": "write_file", "arguments": `{"path":"icon.svg"}`},
	}, "", nil)
	p, _ := newQuiverTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		events := []map[string]interface{}{
			{"type": "response.output_item.added", "sequence_number": 0, "output_index": 0, "item": map[string]interface{}{
				"id": "fc_1", "type": "function_call", "status": "in_progress", "call_id": "call_1", "name": "write_file", "arguments": "",
			}},
			{"type": "response.function_call_arguments.delta", "sequence_number": 1, "item_id": "fc_1", "output_index": 0, "call_id": "call_1", "delta": `{"path":`},
			{"type": "response.function_call_arguments.done", "sequence_number": 2, "item_id": "fc_1", "output_index": 0, "call_id": "call_1", "arguments": `{"path":"icon.svg"}`},
			{"type": "response.output_item.done", "sequence_number": 3, "output_index": 0, "item": completed["output"].([]map[string]interface{})[0]},
			{"type": "response.completed", "sequence_number": 4, "response": completed},
		}
		writeSSE(w, events)
	})
	model, err := p.LanguageModel(ModelArrow2Telos)
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: quiverPrompt("Create an icon.")})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	chunks := drainStream(t, stream)

	var toolCallChunk, finishChunk *provider.StreamChunk
	for i := range chunks {
		if chunks[i].Type == provider.ChunkTypeToolCall {
			toolCallChunk = &chunks[i]
		}
		if chunks[i].Type == provider.ChunkTypeFinish {
			finishChunk = &chunks[i]
		}
	}
	if toolCallChunk == nil || toolCallChunk.ToolCall.ID != "call_1" || toolCallChunk.ToolCall.ToolName != "write_file" {
		t.Fatalf("tool call chunk = %+v", toolCallChunk)
	}
	if toolCallChunk.ToolCall.RawArguments != `{"path":"icon.svg"}` {
		t.Fatalf("RawArguments = %q", toolCallChunk.ToolCall.RawArguments)
	}
	if finishChunk == nil || finishChunk.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("finish chunk = %+v", finishChunk)
	}
}

func TestLanguageModelStreamsCustomToolCalls(t *testing.T) {
	completed := quiverResponse([]map[string]interface{}{
		{"id": "ct_1", "type": "custom_tool_call", "status": "completed", "call_id": "call_custom_1", "name": "write_svg", "input": "<svg />"},
	}, "", nil)
	p, _ := newQuiverTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		events := []map[string]interface{}{
			{"type": "response.output_item.added", "sequence_number": 0, "output_index": 0, "item": map[string]interface{}{
				"id": "ct_1", "type": "custom_tool_call", "status": "in_progress", "call_id": "call_custom_1", "name": "write_svg", "input": "",
			}},
			{"type": "response.custom_tool_call_input.delta", "sequence_number": 1, "item_id": "ct_1", "output_index": 0, "delta": "<svg"},
			{"type": "response.custom_tool_call_input.delta", "sequence_number": 2, "item_id": "ct_1", "output_index": 0, "delta": " />"},
			{"type": "response.output_item.done", "sequence_number": 3, "output_index": 0, "item": completed["output"].([]map[string]interface{})[0]},
			{"type": "response.completed", "sequence_number": 4, "response": completed},
		}
		writeSSE(w, events)
	})
	model, err := p.LanguageModel(ModelArrow2)
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: quiverPrompt("Create an icon.")})
	if err != nil {
		t.Fatalf("DoStream: %v", err)
	}
	chunks := drainStream(t, stream)

	var startChunk, endChunk, toolCallChunk *provider.StreamChunk
	var deltas []string
	for i := range chunks {
		c := &chunks[i]
		switch c.Type {
		case provider.ChunkTypeToolInputStart:
			startChunk = c
		case provider.ChunkTypeToolInputDelta:
			deltas = append(deltas, c.Text)
		case provider.ChunkTypeToolInputEnd:
			endChunk = c
		case provider.ChunkTypeToolCall:
			toolCallChunk = c
		}
	}
	if startChunk == nil || startChunk.ID != "call_custom_1" || startChunk.ToolCall.ToolName != "write_svg" {
		t.Fatalf("tool-input-start = %+v", startChunk)
	}
	if len(deltas) != 2 || deltas[0] != "<svg" || deltas[1] != " />" {
		t.Fatalf("deltas = %+v", deltas)
	}
	if endChunk == nil || endChunk.ID != "call_custom_1" {
		t.Fatalf("tool-input-end = %+v", endChunk)
	}
	if toolCallChunk == nil || toolCallChunk.ToolCall.ID != "call_custom_1" || toolCallChunk.ToolCall.ToolName != "write_svg" {
		t.Fatalf("tool-call = %+v", toolCallChunk)
	}
	assertRawArgumentsDecodeTo(t, toolCallChunk.ToolCall.RawArguments, "<svg />")
}

// assertRawArgumentsDecodeTo decodes raw (a JSON string literal, e.g. a
// custom tool call's RawArguments) and checks it equals want. Comparing
// after decoding, rather than raw bytes, tolerates Go's encoding/json
// HTML-escaping '<'/'>'/'&' by default (unlike TS's JSON.stringify) --
// both encodings are valid JSON for the same string.
func assertRawArgumentsDecodeTo(t *testing.T, raw, want string) {
	t.Helper()
	var got string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("RawArguments %q is not valid JSON: %v", raw, err)
	}
	if got != want {
		t.Fatalf("RawArguments decodes to %q, want %q", got, want)
	}
}

func TestLanguageModelHTTPErrorHandling(t *testing.T) {
	p, _ := newQuiverTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": 503, "code": "service_unavailable", "message": "Model capacity is temporarily unavailable.", "request_id": "req_1",
		})
	})
	model, err := p.LanguageModel(ModelArrow2)
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	_, callErr := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: quiverPrompt("Create an icon.")})
	var perr *providererrors.ProviderError
	if !errors.As(callErr, &perr) {
		t.Fatalf("expected *ProviderError, got %T: %v", callErr, callErr)
	}
	if perr.Message != "Model capacity is temporarily unavailable." {
		t.Fatalf("Message = %q", perr.Message)
	}
	if perr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("StatusCode = %d", perr.StatusCode)
	}
	if !perr.IsRetryable() {
		t.Fatalf("expected retryable error")
	}
	apiErr, ok := perr.Data.(quiverAPIError)
	if !ok || apiErr.RequestID != "req_1" || apiErr.Code != "service_unavailable" {
		t.Fatalf("Data = %+v (ok=%v)", perr.Data, ok)
	}
}

func TestLanguageModelResponseErrorStatusCodeClassification(t *testing.T) {
	p, _ := newQuiverTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(quiverResponse(nil, "failed", map[string]interface{}{
			"error": map[string]interface{}{"code": "service_unavailable", "message": "Try again later.", "status_code": 503},
		}))
	})
	model, err := p.LanguageModel(ModelArrow2)
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	_, callErr := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: quiverPrompt("Create an icon.")})
	var perr *providererrors.ProviderError
	if !errors.As(callErr, &perr) {
		t.Fatalf("expected *ProviderError, got %T: %v", callErr, callErr)
	}
	if perr.Message != "Try again later." {
		t.Fatalf("Message = %q", perr.Message)
	}
	if perr.StatusCode != 503 {
		t.Fatalf("StatusCode = %d, want 503 (from error.status_code)", perr.StatusCode)
	}
	if !perr.IsRetryable() {
		t.Fatalf("expected retryable error")
	}
}

func TestLanguageModelStreamedFailureEvents(t *testing.T) {
	for _, eventType := range []string{"response.failed", "error"} {
		t.Run(eventType, func(t *testing.T) {
			respErr := map[string]interface{}{"code": "service_unavailable", "message": "Try again later.", "status_code": 503}
			var events []map[string]interface{}
			if eventType == "response.failed" {
				events = []map[string]interface{}{
					{"type": "response.failed", "sequence_number": 0, "response": quiverResponse(nil, "failed", map[string]interface{}{"error": respErr})},
				}
			} else {
				events = []map[string]interface{}{
					{"type": "error", "sequence_number": 0, "error": respErr},
				}
			}
			p, _ := newQuiverTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				writeSSE(w, events)
			})
			model, err := p.LanguageModel(ModelArrow2)
			if err != nil {
				t.Fatalf("LanguageModel: %v", err)
			}
			stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{Prompt: quiverPrompt("Create an icon.")})
			if err != nil {
				t.Fatalf("DoStream: %v", err)
			}
			chunks := drainStream(t, stream)

			var errChunk *provider.StreamChunk
			for i := range chunks {
				if chunks[i].Type == provider.ChunkTypeError {
					errChunk = &chunks[i]
				}
			}
			if errChunk == nil {
				t.Fatalf("no error chunk in stream: %+v", chunks)
			}
			var streamErr *providererrors.StreamProviderError
			if !errors.As(errChunk.Err, &streamErr) {
				t.Fatalf("expected *StreamProviderError, got %T: %v", errChunk.Err, errChunk.Err)
			}
			if streamErr.StatusCode == nil || *streamErr.StatusCode != 503 {
				t.Fatalf("StatusCode = %v", streamErr.StatusCode)
			}
			if !streamErr.IsRetryable {
				t.Fatalf("expected retryable stream error")
			}
		})
	}
}

func TestLanguageModelCancelsRequest(t *testing.T) {
	started := make(chan struct{})
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-block:
		}
	}))
	defer server.Close()
	defer close(block)

	p := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	model, err := p.LanguageModel(ModelArrow2)
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := model.DoGenerate(ctx, &provider.GenerateOptions{Prompt: quiverPrompt("Create an icon.")})
		errCh <- err
	}()

	<-started
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DoGenerate did not return after cancellation")
	}
}

func TestLanguageModelWorkflowSerialization(t *testing.T) {
	p := New(Config{APIKey: "test-api-key", BaseURL: "https://example.com/quiverai", Headers: map[string]string{"x-custom": "custom-value"}})
	model, err := p.LanguageModel("arrow-2-telos")
	if err != nil {
		t.Fatalf("LanguageModel: %v", err)
	}

	serialized, err := provider.SerializeModel(model)
	if err != nil {
		t.Fatalf("SerializeModel: %v", err)
	}
	if serialized.Provider != "quiverai.responses" || serialized.ModelID != "arrow-2-telos" {
		t.Fatalf("serialized = %+v", serialized)
	}
	// Go-specific: APIKey is intentionally excluded from the serialized
	// config (see package doc comment above and
	// pkg/providers/openresponses/serialization_test.go).
	if _, hasAPIKey := serialized.Config["apiKey"]; hasAPIKey {
		t.Fatalf("apiKey should not be serialized: %+v", serialized.Config)
	}

	restored, err := provider.DeserializeModel(serialized)
	if err != nil {
		t.Fatalf("DeserializeModel: %v", err)
	}
	if restored.ModelID() != "arrow-2-telos" || restored.Provider() != "quiverai.responses" {
		t.Fatalf("restored model = provider:%q id:%q", restored.Provider(), restored.ModelID())
	}

	// The restored model still applies QuiverAI's own request policy
	// (reasoning validation) even though it crossed the "workflow boundary".
	rlm, ok := restored.(*LanguageModel)
	if !ok {
		t.Fatalf("restored model type = %T, want *quiverai.LanguageModel", restored)
	}
	_, callErr := rlm.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:          quiverPrompt("Create an icon."),
		ProviderOptions: map[string]interface{}{"quiverai": map[string]interface{}{"reasoningEffort": "max"}},
	})
	if !providererrors.IsInvalidArgumentError(callErr) {
		t.Fatalf("expected InvalidArgumentError after restore, got %v", callErr)
	}
}

// drainStream reads every chunk from stream until io.EOF, closing it
// afterward.
func drainStream(t *testing.T, stream provider.TextStream) []provider.StreamChunk {
	t.Helper()
	defer stream.Close()
	var chunks []provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("stream.Next(): %v", err)
		}
		chunks = append(chunks, *chunk)
	}
	return chunks
}

// writeSSE writes a sequence of Open Responses SSE events followed by the
// [DONE] sentinel, mirroring the TS test fixtures' `data: ...\n\n` chunks.
func writeSSE(w http.ResponseWriter, events []map[string]interface{}) {
	for _, event := range events {
		data, _ := json.Marshal(event)
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(data)
		_, _ = w.Write([]byte("\n\n"))
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
}

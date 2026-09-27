package ai

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestToolResultsToContentPartsIncludesErrorMetadata(t *testing.T) {
	parts := toolResultsToContentParts([]types.ToolResult{{
		ToolCallID: "call-1",
		ToolName:   "lookup",
		Error:      errors.New("network failure"),
	}})
	if len(parts) != 1 {
		t.Fatalf("len(parts) = %d, want 1", len(parts))
	}
	tr, ok := parts[0].(types.ToolErrorContent)
	if !ok {
		t.Fatalf("part type = %T, want ToolErrorContent", parts[0])
	}
	if tr.Error != "network failure" || tr.ToolCallID != "call-1" || tr.ToolName != "lookup" {
		t.Fatalf("unexpected tool error content: %+v", tr)
	}
	if tr.ContentType() != "tool-error" {
		t.Fatalf("ContentType() = %q, want tool-error", tr.ContentType())
	}
}

func TestGenerateResultContentPartsSynthesizesTextAndToolCalls(t *testing.T) {
	parts := generateResultContentParts(&types.GenerateResult{
		Text: "Using a tool",
		ToolCalls: []types.ToolCall{{
			ID:               "call-1",
			ToolName:         "lookup",
			Title:            "Lookup",
			Arguments:        map[string]interface{}{"q": "docs"},
			ProviderExecuted: true,
			ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"trace": "abc"}},
			ToolMetadata:     map[string]interface{}{"source": "catalog"},
			Dynamic:          true,
			Invalid:          true,
			Error:            errors.New("bad input"),
		}},
	})

	if len(parts) != 2 {
		t.Fatalf("len(parts) = %d, want 2", len(parts))
	}
	if text, ok := parts[0].(types.TextContent); !ok || text.Text != "Using a tool" {
		t.Fatalf("parts[0] = %#v, want text", parts[0])
	}
	call, ok := parts[1].(types.ToolCallContent)
	if !ok {
		t.Fatalf("parts[1] = %T, want ToolCallContent", parts[1])
	}
	if call.ToolCallID != "call-1" ||
		call.Title != "Lookup" ||
		!call.ProviderExecuted ||
		len(call.ProviderMetadata) == 0 ||
		call.ToolMetadata["source"] != "catalog" ||
		!call.Dynamic ||
		!call.Invalid ||
		call.Error != "bad input" {
		t.Fatalf("unexpected tool-call content: %+v", call)
	}
}

func TestGenerateResultContentPartsReplacesToolCallWithCanonicalStepCall(t *testing.T) {
	parts := generateResultContentParts(&types.GenerateResult{
		Content: []types.ContentPart{
			types.ToolCallContent{
				ToolCallID:       "call-1",
				ToolName:         "lookup",
				Input:            `{"q":"stale"}`,
				Arguments:        map[string]interface{}{"q": "stale"},
				ProviderExecuted: false,
			},
		},
		ToolCalls: []types.ToolCall{{
			ID:               "call-1",
			ToolName:         "lookup",
			RawArguments:     `{"q":"refined"}`,
			Arguments:        map[string]interface{}{"q": "refined"},
			ProviderExecuted: true,
			ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"trace": "abc"}},
		}},
	})

	if len(parts) != 1 {
		t.Fatalf("len(parts) = %d, want 1", len(parts))
	}
	call, ok := parts[0].(types.ToolCallContent)
	if !ok {
		t.Fatalf("parts[0] = %T, want ToolCallContent", parts[0])
	}
	if call.Input != `{"q":"refined"}` || call.Arguments["q"] != "refined" || !call.ProviderExecuted || len(call.ProviderMetadata) == 0 {
		t.Fatalf("tool-call content was not replaced with canonical step tool call: %+v", call)
	}
}

func TestGenerateResultContentPartsEnrichesProviderToolResultWithToolCallInput(t *testing.T) {
	parts := generateResultContentParts(&types.GenerateResult{
		Content: []types.ContentPart{
			types.ToolResultContent{
				ToolCallID:       "call-1",
				ToolName:         "lookup",
				Output:           &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"ok": true}},
				ProviderExecuted: true,
			},
			types.ToolErrorContent{
				ToolCallID:       "call-2",
				ToolName:         "dynamic_lookup",
				Error:            map[string]interface{}{"message": "failed"},
				ProviderExecuted: true,
			},
		},
		ToolCalls: []types.ToolCall{
			{
				ID:           "call-1",
				ToolName:     "lookup",
				Title:        "Lookup",
				Arguments:    map[string]interface{}{"q": "docs"},
				ToolMetadata: map[string]interface{}{"source": "catalog"},
			},
			{
				ID:           "call-2",
				ToolName:     "dynamic_lookup",
				Title:        "Dynamic Lookup",
				Arguments:    map[string]interface{}{"q": "dynamic"},
				ToolMetadata: map[string]interface{}{"source": "dynamic-catalog"},
				Dynamic:      true,
			},
		},
	})

	var result types.ToolResultContent
	var foundResult bool
	for _, part := range parts {
		if p, ok := part.(types.ToolResultContent); ok && p.ToolCallID == "call-1" {
			result = p
			foundResult = true
			break
		}
	}
	if !foundResult {
		t.Fatalf("missing enriched ToolResultContent in %#v", parts)
	}
	if result.Title != "" || result.Input["q"] != "docs" || result.ToolMetadata["source"] != "catalog" || result.Dynamic {
		t.Fatalf("tool result was not enriched from tool call: %+v", result)
	}
	var errPart types.ToolErrorContent
	var foundError bool
	for _, part := range parts {
		if p, ok := part.(types.ToolErrorContent); ok && p.ToolCallID == "call-2" {
			errPart = p
			foundError = true
			break
		}
	}
	if !foundError {
		t.Fatalf("missing enriched ToolErrorContent in %#v", parts)
	}
	if errPart.Title != "" || errPart.Input["q"] != "dynamic" || errPart.ToolMetadata["source"] != "dynamic-catalog" || !errPart.Dynamic {
		t.Fatalf("tool error was not enriched from tool call: %+v", errPart)
	}
}

func TestToolResultsToContentPartsIncludesToolResultInputAndMetadata(t *testing.T) {
	parts := toolResultsToContentParts([]types.ToolResult{{
		ToolCallID:       "call-1",
		ToolName:         "lookup",
		Input:            map[string]interface{}{"q": "docs"},
		Title:            "Lookup",
		Result:           "found",
		ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"trace": "abc"}},
		ToolMetadata:     map[string]interface{}{"source": "catalog"},
		Dynamic:          true,
		Preliminary:      true,
	}})

	if len(parts) != 1 {
		t.Fatalf("len(parts) = %d, want 1", len(parts))
	}
	result, ok := parts[0].(types.ToolResultContent)
	if !ok {
		t.Fatalf("parts[0] = %T, want ToolResultContent", parts[0])
	}
	if result.Title != "" || result.Input["q"] != "docs" || result.ToolMetadata["source"] != "catalog" || !result.Dynamic || !result.Preliminary || len(result.ProviderMetadata) == 0 {
		t.Fatalf("tool result missing input/metadata: %+v", result)
	}
}

func TestToolResultsToContentPartsIncludesUserApprovalRequestOnly(t *testing.T) {
	parts := toolResultsToContentParts([]types.ToolResult{{
		ToolCallID:     "call-1",
		ApprovalID:     "approval-1",
		ToolName:       "lookup",
		Input:          map[string]interface{}{"city": "Tokyo"},
		ApprovalStatus: types.ToolApprovalStatusUserApproval,
	}})

	if len(parts) != 1 {
		t.Fatalf("len(parts) = %d, want 1", len(parts))
	}
	req, ok := parts[0].(types.ToolApprovalRequestContent)
	if !ok {
		t.Fatalf("part type = %T, want ToolApprovalRequestContent", parts[0])
	}
	if req.ApprovalID != "approval-1" || req.ToolCall.ID != "call-1" || req.ToolCall.ToolName != "lookup" {
		t.Fatalf("unexpected approval request: %+v", req)
	}
	if req.IsAutomatic {
		t.Fatalf("IsAutomatic = true, want false for user approval request")
	}
	requestJSON, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal approval request: %v", err)
	}
	var requestObject map[string]interface{}
	if err := json.Unmarshal(requestJSON, &requestObject); err != nil {
		t.Fatalf("unmarshal approval request: %v", err)
	}
	if _, ok := requestObject["toolCallId"]; ok {
		t.Fatalf("public approval request JSON should not include top-level toolCallId, got %s", requestJSON)
	}
	if requestObject["type"] != "tool-approval-request" {
		t.Fatalf("public approval request JSON missing TS type discriminator: %s", requestJSON)
	}
	if !strings.Contains(string(requestJSON), `"toolCall":{"type":"tool-call","toolCallId":"call-1","toolName":"lookup","input":{"city":"Tokyo"}}`) {
		t.Fatalf("public approval request JSON should use TS nested toolCall shape, got %s", requestJSON)
	}
}

func TestToolResultsToContentPartsOrdersAutomaticApprovalBeforeResult(t *testing.T) {
	reason := "safe"
	parts := toolResultsToContentParts([]types.ToolResult{{
		ToolCallID:       "call-1",
		ToolName:         "lookup",
		Input:            map[string]interface{}{"city": "Tokyo"},
		Result:           "sunny",
		ApprovalStatus:   types.ToolApprovalStatusApproved,
		ApprovalReason:   &reason,
		ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"trace": "abc"}},
	}})

	if len(parts) != 3 {
		t.Fatalf("len(parts) = %d, want 3", len(parts))
	}
	req, ok := parts[0].(types.ToolApprovalRequestContent)
	if !ok {
		t.Fatalf("parts[0] = %T, want ToolApprovalRequestContent", parts[0])
	}
	if !req.IsAutomatic {
		t.Fatalf("automatic approval request IsAutomatic = false, want true")
	}
	resp, ok := parts[1].(types.ToolApprovalResponseContent)
	if !ok {
		t.Fatalf("parts[1] = %T, want ToolApprovalResponseContent", parts[1])
	}
	if !resp.Approved || resp.Reason != "safe" || resp.ToolCall.ID != "call-1" {
		t.Fatalf("unexpected approval response: %+v", resp)
	}
	responseJSON, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal approval response: %v", err)
	}
	var responseObject map[string]interface{}
	if err := json.Unmarshal(responseJSON, &responseObject); err != nil {
		t.Fatalf("unmarshal approval response: %v", err)
	}
	if _, ok := responseObject["toolCallId"]; ok {
		t.Fatalf("public approval response JSON should not include top-level toolCallId, got %s", responseJSON)
	}
	if responseObject["type"] != "tool-approval-response" {
		t.Fatalf("public approval response JSON missing TS type discriminator: %s", responseJSON)
	}
	toolCallObject, _ := responseObject["toolCall"].(map[string]interface{})
	if toolCallObject["toolCallId"] != "call-1" || toolCallObject["toolName"] != "lookup" {
		t.Fatalf("public approval response JSON should use TS nested toolCall shape, got %s", responseJSON)
	}
	tr, ok := parts[2].(types.ToolResultContent)
	if !ok {
		t.Fatalf("parts[2] = %T, want ToolResultContent", parts[2])
	}
	if tr.ToolCallID != "call-1" {
		t.Fatalf("unexpected tool result: %+v", tr)
	}
	if tr.Result != nil || tr.Output == nil || tr.Output.Type != types.ToolResultOutputText || tr.Output.Value != "sunny" {
		t.Fatalf("unexpected model output: result=%#v output=%+v", tr.Result, tr.Output)
	}
	if len(tr.ProviderMetadata) == 0 {
		t.Fatalf("provider metadata was not forwarded to tool result content: %+v", tr)
	}
}

func TestToolResultsToContentPartsOrdersApprovedToolErrorAfterApproval(t *testing.T) {
	reason := "safe"
	parts := toolResultsToContentParts([]types.ToolResult{{
		ToolCallID:       "call-1",
		ToolName:         "lookup",
		Input:            map[string]interface{}{"city": "Tokyo"},
		Error:            errors.New("network failure"),
		ApprovalStatus:   types.ToolApprovalStatusApproved,
		ApprovalReason:   &reason,
		ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"trace": "err"}},
	}})

	if len(parts) != 3 {
		t.Fatalf("len(parts) = %d, want 3", len(parts))
	}
	if _, ok := parts[0].(types.ToolApprovalRequestContent); !ok {
		t.Fatalf("parts[0] = %T, want ToolApprovalRequestContent", parts[0])
	}
	if _, ok := parts[1].(types.ToolApprovalResponseContent); !ok {
		t.Fatalf("parts[1] = %T, want ToolApprovalResponseContent", parts[1])
	}
	errPart, ok := parts[2].(types.ToolErrorContent)
	if !ok {
		t.Fatalf("parts[2] = %T, want ToolErrorContent", parts[2])
	}
	if errPart.Error != "network failure" || errPart.ToolCallID != "call-1" {
		t.Fatalf("unexpected tool error: %+v", errPart)
	}
	if len(errPart.ProviderMetadata) == 0 {
		t.Fatalf("provider metadata was not forwarded to tool error content: %+v", errPart)
	}
}

func TestToolResultsToContentPartsIncludesDeniedApprovalWithoutToolResult(t *testing.T) {
	reason := "not allowed"
	parts := toolResultsToContentParts([]types.ToolResult{{
		ToolCallID:     "call-1",
		ToolName:       "lookup",
		ApprovalStatus: types.ToolApprovalStatusDenied,
		ApprovalReason: &reason,
		Error:          errors.New("tool execution denied: not allowed"),
	}})

	if len(parts) != 2 {
		t.Fatalf("len(parts) = %d, want 2", len(parts))
	}
	if _, ok := parts[0].(types.ToolApprovalRequestContent); !ok {
		t.Fatalf("parts[0] = %T, want ToolApprovalRequestContent", parts[0])
	}
	resp, ok := parts[1].(types.ToolApprovalResponseContent)
	if !ok {
		t.Fatalf("parts[1] = %T, want ToolApprovalResponseContent", parts[1])
	}
	if resp.Approved || resp.Reason != "not allowed" {
		t.Fatalf("unexpected denied approval response: %+v", resp)
	}
}

func TestToolResultsToContentPartsIncludesProviderExecutedApprovalResponse(t *testing.T) {
	reason := "safe"
	parts := toolResultsToContentParts([]types.ToolResult{{
		ToolCallID:       "call-1",
		ToolName:         "server_tool",
		Input:            map[string]interface{}{"query": "docs"},
		ApprovalStatus:   types.ToolApprovalStatusApproved,
		ApprovalReason:   &reason,
		ProviderExecuted: true,
	}})

	if len(parts) != 2 {
		t.Fatalf("len(parts) = %d, want 2", len(parts))
	}
	req, ok := parts[0].(types.ToolApprovalRequestContent)
	if !ok {
		t.Fatalf("parts[0] = %T, want ToolApprovalRequestContent", parts[0])
	}
	if !req.ToolCall.ProviderExecuted {
		t.Fatalf("approval request toolCall ProviderExecuted = false, want true")
	}
	resp, ok := parts[1].(types.ToolApprovalResponseContent)
	if !ok {
		t.Fatalf("parts[1] = %T, want ToolApprovalResponseContent", parts[1])
	}
	if !resp.Approved || !resp.ProviderExecuted || resp.Reason != "safe" {
		t.Fatalf("unexpected approval response: %+v", resp)
	}
}

func TestToolResultsToContentPartsIncludesProviderExecutedDeniedApproval(t *testing.T) {
	reason := "policy"
	parts := toolResultsToContentParts([]types.ToolResult{{
		ToolCallID:       "call-1",
		ToolName:         "server_tool",
		ApprovalStatus:   types.ToolApprovalStatusDenied,
		ApprovalReason:   &reason,
		ProviderExecuted: true,
	}})

	if len(parts) != 2 {
		t.Fatalf("len(parts) = %d, want 2", len(parts))
	}
	resp, ok := parts[1].(types.ToolApprovalResponseContent)
	if !ok {
		t.Fatalf("parts[1] = %T, want ToolApprovalResponseContent", parts[1])
	}
	if resp.Approved || !resp.ProviderExecuted || resp.Reason != "policy" {
		t.Fatalf("unexpected denied approval response: %+v", resp)
	}
}

func TestExecuteToolsResolvesApprovalForProviderExecutedTools(t *testing.T) {
	reason := "server tool approved"
	results, err := executeTools(
		context.Background(),
		[]types.ToolCall{{ID: "call-1", ToolName: "server_tool", Arguments: map[string]interface{}{"q": "docs"}}},
		[]types.Tool{{Name: "server_tool", ProviderExecuted: true}},
		nil,
		nil,
		map[string]types.ToolApprovalValue{
			"server_tool": types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved, Reason: &reason},
		},
		&types.Usage{},
		toolCallEventCallbacks{toolExecutionMs: map[string]int64{}},
	)
	if err != nil {
		t.Fatalf("executeTools error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if !results[0].ProviderExecuted || results[0].ApprovalStatus != types.ToolApprovalStatusApproved {
		t.Fatalf("result = %+v, want provider-executed approved result", results[0])
	}
	parts := toolResultsToContentParts(results)
	if len(parts) != 2 {
		t.Fatalf("len(parts) = %d, want approval request/response only", len(parts))
	}
	if resp, ok := parts[1].(types.ToolApprovalResponseContent); !ok || !resp.ProviderExecuted || !resp.Approved {
		t.Fatalf("approval response = %#v, want provider-executed approved response", parts[1])
	}
}

func TestExecuteToolsAppliesToModelOutputWithTSShapedOptions(t *testing.T) {
	raw := map[string]interface{}{"public": "visible", "secret": "hide me"}
	var gotOptions types.ToModelOutputOptions
	results, err := executeTools(
		context.Background(),
		[]types.ToolCall{{
			ID:        "call-1",
			ToolName:  "lookup",
			Arguments: map[string]interface{}{"query": "docs"},
		}},
		[]types.Tool{{
			Name: "lookup",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return raw, nil
			},
			ToModelOutput: func(_ context.Context, opts types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
				gotOptions = opts
				return &types.ToolResultOutput{Type: types.ToolResultOutputText, Value: "model sees: visible"}, nil
			},
		}},
		nil,
		nil,
		nil,
		&types.Usage{},
		toolCallEventCallbacks{toolExecutionMs: map[string]int64{}},
	)
	if err != nil {
		t.Fatalf("executeTools error = %v", err)
	}
	if gotOptions.ToolCallID != "call-1" || gotOptions.Input["query"] != "docs" || !reflect.DeepEqual(gotOptions.Output, raw) {
		t.Fatalf("ToModelOutput options = %+v, want TS-shaped toolCallId/input/output", gotOptions)
	}
	if !reflect.DeepEqual(gotOptions.Result, raw) {
		t.Fatalf("ToModelOutput result alias = %#v, want original map", gotOptions.Result)
	}
	if gotOptions.ToolCall == nil || gotOptions.ToolCall.ID != "call-1" || gotOptions.ToolCall.ToolName != "lookup" {
		t.Fatalf("ToModelOutput ToolCall = %+v, want call-1/lookup", gotOptions.ToolCall)
	}
	if len(results) != 1 || !reflect.DeepEqual(results[0].Result, raw) {
		t.Fatalf("raw tool results = %#v, want original result", results)
	}
	if results[0].ModelOutput == nil || results[0].ModelOutput.Value != "model sees: visible" {
		t.Fatalf("ModelOutput = %+v, want converted output", results[0].ModelOutput)
	}
	parts := toolResultsToContentParts(results)
	if len(parts) != 1 {
		t.Fatalf("len(parts) = %d, want 1", len(parts))
	}
	toolResult, ok := parts[0].(types.ToolResultContent)
	if !ok {
		t.Fatalf("parts[0] = %T, want ToolResultContent", parts[0])
	}
	if toolResult.Result != nil || toolResult.Output == nil || toolResult.Output.Value != "model sees: visible" {
		t.Fatalf("tool result content = %+v, want converted model output only", toolResult)
	}
}

func TestExecuteToolsToModelOutputErrorPropagates(t *testing.T) {
	convertErr := errors.New("conversion failed")
	_, err := executeTools(
		context.Background(),
		[]types.ToolCall{{
			ID:        "call-1",
			ToolName:  "lookup",
			Arguments: map[string]interface{}{"query": "docs"},
		}},
		[]types.Tool{{
			Name: "lookup",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "raw", nil
			},
			ToModelOutput: func(context.Context, types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
				return nil, convertErr
			},
		}},
		nil,
		nil,
		nil,
		&types.Usage{},
		toolCallEventCallbacks{toolExecutionMs: map[string]int64{}},
	)
	if !errors.Is(err, convertErr) {
		t.Fatalf("executeTools error = %v, want conversion error", err)
	}
	if err.Error() != "conversion failed" {
		t.Fatalf("executeTools error string = %q, want original conversion error", err.Error())
	}
}

func TestGenerateTextToModelOutputErrorPropagatesUnwrapped(t *testing.T) {
	convertErr := errors.New("conversion failed")
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				ToolCalls: []types.ToolCall{{
					ID:        "call-1",
					ToolName:  "lookup",
					Arguments: map[string]interface{}{"query": "docs"},
				}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "lookup",
		Tools: []types.Tool{{
			Name: "lookup",
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "raw", nil
			},
			ToModelOutput: func(context.Context, types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
				return nil, convertErr
			},
		}},
		StopWhen: []StopCondition{StepCountIs(2)},
	})
	if !errors.Is(err, convertErr) {
		t.Fatalf("GenerateText error = %v, want conversion error", err)
	}
	if err.Error() != "conversion failed" {
		t.Fatalf("GenerateText error string = %q, want original conversion error", err.Error())
	}
}

// TestToolResultModelOutputNormalizesStructValue ports TS
// createToolModelOutput's toJSONValue behavior (audit row 6aa7c54 / WG24): a
// non-string tool output is round-tripped through JSON so it lands as a
// plain JSON value (map/slice/scalar), not the raw Go struct/time.Time.
func TestToolResultModelOutputNormalizesStructValue(t *testing.T) {
	type payload struct {
		City string    `json:"city"`
		At   time.Time `json:"at"`
	}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	out := toolResultModelOutput(payload{City: "Tokyo", At: at})
	if out.Type != types.ToolResultOutputJSON {
		t.Fatalf("Type = %v, want json", out.Type)
	}
	m, ok := out.Value.(map[string]interface{})
	if !ok {
		t.Fatalf("Value = %T (%#v), want map[string]interface{}", out.Value, out.Value)
	}
	if m["city"] != "Tokyo" {
		t.Fatalf("city = %v, want Tokyo", m["city"])
	}
	if m["at"] != at.Format(time.RFC3339) {
		t.Fatalf("at = %v, want %s", m["at"], at.Format(time.RFC3339))
	}
}

// TestToolResultModelOutputNilBecomesJSONNull mirrors TS's undefined -> null.
func TestToolResultModelOutputNilBecomesJSONNull(t *testing.T) {
	out := toolResultModelOutput(nil)
	if out.Type != types.ToolResultOutputJSON {
		t.Fatalf("Type = %v, want json", out.Type)
	}
	if out.Value != nil {
		t.Fatalf("Value = %#v, want nil", out.Value)
	}
}

// TestAppendTextPart_KeepsProviderMetadata ports TS stream-text.ts's
// `activeText.providerMetadata = part.providerMetadata ??
// activeText.providerMetadata` (latest non-nil metadata wins across merged
// text deltas), and verifies a metadata-only delta (empty text, non-nil
// metadata -- e.g. Gemini/Gateway thoughtSignature deltas) isn't dropped.
func TestAppendTextPart_KeepsProviderMetadata(t *testing.T) {
	meta1 := json.RawMessage(`{"gemini":{"thoughtSignature":"sig-1"}}`)
	meta2 := json.RawMessage(`{"gemini":{"thoughtSignature":"sig-2"}}`)

	var parts []types.ContentPart
	parts = appendTextPart(parts, "Hello ", meta1)
	parts = appendTextPart(parts, "World", nil)
	// Metadata-only delta: empty text, non-nil metadata must still update.
	parts = appendTextPart(parts, "", meta2)

	if len(parts) != 1 {
		t.Fatalf("len(parts) = %d, want 1", len(parts))
	}
	text, ok := parts[0].(types.TextContent)
	if !ok {
		t.Fatalf("parts[0] = %T, want types.TextContent", parts[0])
	}
	if text.Text != "Hello World" {
		t.Errorf("Text = %q, want %q", text.Text, "Hello World")
	}
	// nil-metadata delta ("World") must not clear the previously-seen
	// metadata; the last non-nil delta ("" with meta2) wins.
	if string(text.ProviderMetadata) != string(meta2) {
		t.Errorf("ProviderMetadata = %s, want %s", text.ProviderMetadata, meta2)
	}
}

// TestStreamText_KeepsTextProviderMetadata is an end-to-end regression test
// for the pkg/ai accumulator dropping ProviderMetadata on ChunkTypeText
// chunks (e.g. Gemini/Gateway thoughtSignature): the final step's Content
// must retain the metadata from the text delta chunks, not just the
// concatenated text.
func TestStreamText_KeepsTextProviderMetadata(t *testing.T) {
	meta := json.RawMessage(`{"google":{"thoughtSignature":"abc"}}`)
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "Hello ", ProviderMetadata: meta},
				{Type: provider.ChunkTypeText, Text: "World!"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Say hello",
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	var found *types.TextContent
	for _, part := range result.Content() {
		if tc, ok := part.(types.TextContent); ok {
			found = &tc
		}
	}
	if found == nil {
		t.Fatalf("no TextContent in result.Content(): %+v", result.Content())
	}
	if found.Text != "Hello World!" {
		t.Errorf("Text = %q, want %q", found.Text, "Hello World!")
	}
	if string(found.ProviderMetadata) != string(meta) {
		t.Errorf("ProviderMetadata = %s, want %s", found.ProviderMetadata, meta)
	}
}

// TestStreamText_AdjacentTextStartBoundariesDoNotMerge is an end-to-end
// regression test for two text-like blocks streamed back-to-back with no
// other content between them (e.g. an Anthropic compaction block
// immediately followed by a plain text block): each carries its own
// text-start providerMetadata, and without respecting the text-start
// boundary, appendTextPart's blind trailing-part merge would fold both
// blocks into one TextContent and mislabel the whole merged text with
// whichever block's metadata arrived last. TS keys accumulated text by the
// text-start chunk's id and always starts a fresh record on text-start
// (stream-text.ts activeTextContent), so two text-starts always produce two
// content parts.
func TestStreamText_AdjacentTextStartBoundariesDoNotMerge(t *testing.T) {
	metaA := json.RawMessage(`{"anthropic":{"type":"compaction","signature":"sig-a"}}`)
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "0", ProviderMetadata: metaA},
				{Type: provider.ChunkTypeText, ID: "0", Text: "Summary of the conversation."},
				{Type: provider.ChunkTypeTextEnd, ID: "0", ProviderMetadata: metaA},
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "Here is the answer."},
				{Type: provider.ChunkTypeTextEnd, ID: "1"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "Continue",
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	var textParts []types.TextContent
	for _, part := range result.Content() {
		if tc, ok := part.(types.TextContent); ok {
			textParts = append(textParts, tc)
		}
	}
	if len(textParts) != 2 {
		t.Fatalf("len(textParts) = %d, want 2 (one per text-start boundary): %+v", len(textParts), textParts)
	}
	if textParts[0].Text != "Summary of the conversation." {
		t.Errorf("textParts[0].Text = %q", textParts[0].Text)
	}
	if string(textParts[0].ProviderMetadata) != string(metaA) {
		t.Errorf("textParts[0].ProviderMetadata = %s, want %s", textParts[0].ProviderMetadata, metaA)
	}
	if textParts[1].Text != "Here is the answer." {
		t.Errorf("textParts[1].Text = %q", textParts[1].Text)
	}
	if textParts[1].ProviderMetadata != nil {
		t.Errorf("textParts[1].ProviderMetadata = %s, want nil (must not inherit the compaction block's metadata)", textParts[1].ProviderMetadata)
	}
}

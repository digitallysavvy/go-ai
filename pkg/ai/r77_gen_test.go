package ai

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r77genCountTool is the Go form of the TS tool
// `inputSchema: z.object({ count: z.string().transform(Number) })`: its
// ValidateInput accepts only a string count and returns it as a number.
func r77genCountTool(executed *map[string]interface{}) types.Tool {
	return types.Tool{
		Name: "tool1",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"count": map[string]interface{}{"type": "string"}},
		},
		ValidateInput: func(v interface{}) (interface{}, error) {
			m, _ := v.(map[string]interface{})
			s, ok := m["count"].(string)
			if !ok {
				return nil, errors.New("count must be a string")
			}
			n, err := strconv.Atoi(s)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"count": float64(n)}, nil
		},
		ToolApproval: types.ToolApprovalStatusUserApproval,
		Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			if executed != nil {
				*executed = in
			}
			return "ok", nil
		},
	}
}

func r77genApproval(input, schemaInput map[string]interface{}) CollectedToolApproval {
	call := types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: input}
	req := types.ToolApprovalRequestContent{ApprovalID: "approval-1", ToolCallID: "call-1", ToolCall: call}
	if schemaInput != nil {
		req.InputSchemaInput = schemaInput
	}
	return CollectedToolApproval{
		ApprovalRequest:  req,
		ApprovalResponse: types.ToolApprovalResponseContent{ApprovalID: "approval-1", Approved: true},
		ToolCall:         call,
	}
}

// Port of validate-tool-approvals.test.ts 123, 154, 195 and 260: a schema
// that transforms its input (Tool.ValidateInput) is revalidated from the
// approval's inputSchemaInput and compared with the approved input.
func TestR77Gen_ValidateApprovedToolApprovalsTransformingSchema(t *testing.T) {
	reshape := types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object"},
		ValidateInput: func(v interface{}) (interface{}, error) {
			m, _ := v.(map[string]interface{})
			s, ok := m["raw"].(string)
			if !ok {
				return nil, errors.New("raw must be a string")
			}
			n, _ := strconv.Atoi(s)
			return map[string]interface{}{"count": float64(n)}, nil
		},
		Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	for _, tc := range []struct {
		name        string
		tool        types.Tool
		approval    CollectedToolApproval
		wantInvalid string
	}{
		{name: "transforms to the approved input", tool: r77genCountTool(nil), approval: r77genApproval(map[string]interface{}{"count": 3}, map[string]interface{}{"count": "3"})},
		{name: "missing schema input changes the approved input", tool: reshape, approval: r77genApproval(map[string]interface{}{"raw": "999"}, nil), wantInvalid: "does not match the validated schema output"},
		{name: "reshapes to the approved input", tool: reshape, approval: r77genApproval(map[string]interface{}{"count": 3}, map[string]interface{}{"raw": "3"})},
		{name: "transformed schema output was changed", tool: r77genCountTool(nil), approval: r77genApproval(map[string]interface{}{"count": 4}, map[string]interface{}{"count": "3"}), wantInvalid: "does not match the validated schema output"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ValidateApprovedToolApprovals(context.Background(), ValidateApprovedToolApprovalsOptions{
				ApprovedToolApprovals: []CollectedToolApproval{tc.approval},
				Tools:                 []types.Tool{tc.tool},
			})
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantInvalid == "" {
				if len(res.ApprovedToolApprovals) != 1 || len(res.InvalidToolApprovals) != 0 {
					t.Fatalf("approved=%d invalid=%+v, want 1 approved", len(res.ApprovedToolApprovals), res.InvalidToolApprovals)
				}
				return
			}
			if len(res.ApprovedToolApprovals) != 0 || len(res.InvalidToolApprovals) != 1 {
				t.Fatalf("approved=%d invalid=%d, want 1 invalid", len(res.ApprovedToolApprovals), len(res.InvalidToolApprovals))
			}
			if msg := res.InvalidToolApprovals[0].Error.Error(); !strings.Contains(msg, tc.wantInvalid) {
				t.Fatalf("error = %q, want %q", msg, tc.wantInvalid)
			}
		})
	}
}

// TS parseToolCall sets the approval's inputSchemaInput to the input the
// schema received (parseResult.rawValue), not the schema output: a call
// whose schema transforms its input gets an approval request carrying the
// raw input, and approving it later revalidates from that input and runs
// the tool with the transformed one.
func TestR77Gen_ApprovalInputSchemaInputIsRawInput(t *testing.T) {
	call := types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"count": "3"}, RawArguments: `{"count":"3"}`}
	initial := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "go"}}}}
	check := func(t *testing.T, msgs []types.Message, run func(history []types.Message)) {
		t.Helper()
		var req *types.ToolApprovalRequestContent
		for _, msg := range msgs {
			for _, part := range msg.Content {
				if r, ok := part.(types.ToolApprovalRequestContent); ok {
					req = &r
				}
			}
		}
		if req == nil {
			t.Fatal("no approval request")
		}
		if want := map[string]interface{}{"count": "3"}; !reflect.DeepEqual(req.InputSchemaInput, want) {
			t.Fatalf("InputSchemaInput = %#v, want %#v", req.InputSchemaInput, want)
		}
		history := append(append(append([]types.Message(nil), initial...), msgs...), types.Message{
			Role:    types.RoleTool,
			Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: req.ApprovalID, Approved: true}},
		})
		run(history)
	}
	t.Run("generate", func(t *testing.T) {
		var executed map[string]interface{}
		tool := r77genCountTool(&executed)
		model := &testutil.MockLanguageModel{
			ToolSupport: true,
			DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{call}}, nil
			},
		}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Tools: []types.Tool{tool}, Messages: initial})
		if err != nil {
			t.Fatal(err)
		}
		check(t, res.ResponseMessages, func(history []types.Message) {
			if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: newRecordingModel("done"), Tools: []types.Tool{tool}, Messages: history}); err != nil {
				t.Fatal(err)
			}
		})
		if executed["count"] != float64(3) {
			t.Fatalf("executed with %#v, want count 3", executed)
		}
	})
	t.Run("stream", func(t *testing.T) {
		var executed map[string]interface{}
		tool := r77genCountTool(&executed)
		model := &testutil.MockLanguageModel{
			ToolSupport: true,
			DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
				c := call
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &c},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			},
		}
		res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Tools: []types.Tool{tool}, Messages: initial, OnChunk: func(provider.StreamChunk) {}})
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Steps()
		check(t, res.ResponseMessages(), func(history []types.Message) {
			res2, err := StreamText(context.Background(), StreamTextOptions{Model: newRecordingModel("done"), Tools: []types.Tool{tool}, Messages: history, OnChunk: func(provider.StreamChunk) {}})
			if err != nil {
				t.Fatal(err)
			}
			_ = res2.Steps()
		})
		if executed["count"] != float64(3) {
			t.Fatalf("executed with %#v, want count 3", executed)
		}
	})
}

// TS revalidates every executable tool, since inputSchema is required: a
// tool without Parameters still runs the input refiner and the
// approved-input comparison, so a refiner that changes the approved input
// makes the approval invalid.
func TestR77Gen_ValidateApprovedToolApprovalsToolWithoutParameters(t *testing.T) {
	tool := types.Tool{
		Name: "tool1",
		Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	approval := r77genApproval(map[string]interface{}{"value": "a"}, nil)
	res, err := ValidateApprovedToolApprovals(context.Background(), ValidateApprovedToolApprovalsOptions{
		ApprovedToolApprovals: []CollectedToolApproval{approval},
		Tools:                 []types.Tool{tool},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ApprovedToolApprovals) != 1 {
		t.Fatalf("without a refiner: approved=%d invalid=%+v, want 1 approved", len(res.ApprovedToolApprovals), res.InvalidToolApprovals)
	}
	res, err = ValidateApprovedToolApprovals(context.Background(), ValidateApprovedToolApprovalsOptions{
		ApprovedToolApprovals: []CollectedToolApproval{approval},
		Tools:                 []types.Tool{tool},
		RefineToolInput: map[string]ToolInputRefiner{
			"tool1": func(ctx context.Context, opts ToolInputRefinementOptions) (map[string]interface{}, error) {
				return map[string]interface{}{"value": opts.ToolCall.Arguments["value"].(string) + "!"}, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ApprovedToolApprovals) != 0 || len(res.InvalidToolApprovals) != 1 {
		t.Fatalf("approved=%d invalid=%d, want 1 invalid", len(res.ApprovedToolApprovals), len(res.InvalidToolApprovals))
	}
	if msg := res.InvalidToolApprovals[0].Error.Error(); msg != "Invalid input for tool tool1: Approved tool input does not match the validated schema output." {
		t.Fatalf("error = %q", msg)
	}
}

// The TypeValidationError value of a ValidateInput failure is TS
// JSON.stringify(JSON.parse(input)): array-index keys first, a repeated key
// at its first position with its last value, strings re-encoded.
func TestR77Gen_ValidateInputErrorValueIsJSStringify(t *testing.T) {
	tool := types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object"},
		ValidateInput: func(interface{}) (interface{}, error) {
			return nil, errors.New("nope")
		},
	}
	got, err := ParseToolCall(context.Background(), ParseToolCallOptions{
		ToolCall: types.ToolCall{ID: "c1", ToolName: "tool1", RawArguments: `{"b": 1, "a": "é", "b": 2, "1": true}`},
		Tools:    []types.Tool{tool},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "Invalid input for tool tool1: AI_TypeValidationError: Type validation failed: Value: {\"1\":true,\"b\":2,\"a\":\"é\"}.\nError message: Error: nope"
	if !got.Invalid || got.Error == nil || got.Error.Error() != want {
		t.Fatalf("error = %v\nwant %q", got.Error, want)
	}
}

// TS generateText builds the tool-error of an invalid call without the
// call's toolMetadata, while the invalid tool-call part keeps it
// (parseToolCall). streamText keeps it on both (streamLanguageModelCall).
func TestR77Gen_InvalidToolCallErrorWithoutToolMetadata(t *testing.T) {
	tool := types.Tool{
		Name:       "tool1",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}},
		Metadata:   map[string]interface{}{"owner": "team"},
		Execute: func(ctx context.Context, in map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	calls := 0
	model := &testutil.MockLanguageModel{
		ToolSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			calls++
			if calls > 1 {
				return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
			}
			return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "tool1", RawArguments: `{}`}}}, nil
		},
	}
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "go", Tools: []types.Tool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	var sawCall, sawErr bool
	for _, part := range res.Steps[0].Content {
		switch p := part.(type) {
		case types.ToolCallContent:
			sawCall = true
			if !p.Invalid || p.ToolMetadata["owner"] != "team" {
				t.Fatalf("tool call = %#v, want invalid with tool metadata", p)
			}
		case types.ToolErrorContent:
			sawErr = true
			if p.ToolMetadata != nil {
				t.Fatalf("tool error ToolMetadata = %#v, want none", p.ToolMetadata)
			}
		}
	}
	if !sawCall || !sawErr {
		t.Fatalf("content = %#v", res.Steps[0].Content)
	}
}

// Port of stream-text.test.ts 29959 "should execute transformed approved
// input after a persisted UI message round trip" with the TS schema
// transform as Tool.ValidateInput (z.string().transform(Number)), no refiner:
// the approval request carries the raw input as inputSchemaInput, which
// survives the UI stream, a JSON round trip, ValidateUIMessages and
// ConvertToModelMessages, and the resumed run executes once with count 3.
func TestR77Gen_TransformedApprovedInputPersistedRoundTrip(t *testing.T) {
	var executed []map[string]interface{}
	countTool := r77genCountTool(nil)
	countTool.Name = "count"
	countTool.ToolApproval = nil
	countTool.Execute = func(_ context.Context, args map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		executed = append(executed, args)
		return args["count"], nil
	}
	tools := []types.Tool{countTool}
	approval := map[string]types.ToolApprovalValue{"count": types.ToolApprovalStatusUserApproval}
	first, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "count-call", ToolName: "count", RawArguments: `{"count":"3"}`}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
		}),
		Prompt: "Count three items.", Tools: tools, ToolApproval: approval,
		Internal: r72streamIDs(), OnError: func(context.Context, error) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	ui, _ := first.ToUIMessageStream(context.Background())
	msgs, errs := ReadUIMessages(context.Background(), ReadUIMessagesOptions{Stream: ui})
	var last UIMessage
	for m := range msgs {
		last = m
	}
	for e := range errs {
		t.Fatalf("read: %v", e)
	}
	raw, err := json.Marshal(last)
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]interface{}
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	var toolPart map[string]interface{}
	for _, p := range persisted["parts"].([]interface{}) {
		if m := p.(map[string]interface{}); m["type"] == "tool-count" {
			toolPart = m
		}
	}
	if toolPart == nil || toolPart["state"] != "approval-requested" {
		t.Fatalf("persisted message = %s", raw)
	}
	if got := toolPart["approval"].(map[string]interface{})["inputSchemaInput"]; !reflect.DeepEqual(got, map[string]interface{}{"count": "3"}) {
		t.Fatalf("approval.inputSchemaInput = %#v, want the raw input", got)
	}
	toolPart["state"] = "approval-responded"
	toolPart["approval"].(map[string]interface{})["approved"] = true
	validated, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: []interface{}{persisted}, Tools: tools})
	if err != nil {
		t.Fatalf("ValidateUIMessages: %v", err)
	}
	modelMessages, err := ConvertToModelMessages(context.Background(), validated, ConvertToModelMessagesOptions{Tools: tools})
	if err != nil {
		t.Fatalf("ConvertToModelMessages: %v", err)
	}
	second, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "Done"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
		}),
		Messages: append([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Count three items."}}}}, modelMessages...),
		Tools:    tools, ToolApproval: approval,
		OnError: func(_ context.Context, err error) { t.Errorf("onError: %v", err) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if text := second.Text(); text != "Done" {
		t.Fatalf("text = %q (err %v)", text, second.Err())
	}
	if len(executed) != 1 || !reflect.DeepEqual(executed[0], map[string]interface{}{"count": float64(3)}) {
		t.Fatalf("execute calls = %v, want one with count 3", executed)
	}
}

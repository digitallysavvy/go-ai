package ai

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r72 stream CR-1: GenerateText's step-end event is the StepResult (TS
// GenerateTextStepEndEvent = StepResult: content, performance and
// request.messages), and the end event's request is the final step's.
func TestR72Integ_GenerateTextStepEventCarriesContentPerformanceRequest(t *testing.T) {
	var mu sync.Mutex
	var events []OnStepFinishEvent
	var end OnFinishEvent
	res, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: r72genSeqModel(t,
			r72genCalls(types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "v"}}),
			r72genText("done")),
		Prompt:   "test-input",
		Tools:    []types.Tool{r72genValueTool("tool1")},
		StopWhen: []StopCondition{StepCountIs(3)},
		Include:  &IncludeOptions{RequestMessages: true},
		OnStepFinishEvent: func(_ context.Context, e OnStepFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, e)
		},
		OnEndEvent: func(_ context.Context, e OnFinishEvent) {
			mu.Lock()
			defer mu.Unlock()
			end = e
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 || len(res.Steps) != 2 {
		t.Fatalf("events = %d, steps = %d", len(events), len(res.Steps))
	}
	for i, e := range events {
		if len(e.Content) == 0 || !reflect.DeepEqual(e.Content, res.Steps[i].Content) {
			t.Errorf("event %d content = %#v, want %#v", i, e.Content, res.Steps[i].Content)
		}
		if !reflect.DeepEqual(e.Performance, res.Steps[i].Performance) {
			t.Errorf("event %d performance = %#v, want %#v", i, e.Performance, res.Steps[i].Performance)
		}
		if len(e.Request.Messages) == 0 || !reflect.DeepEqual(e.Request.Messages, res.Steps[i].Request.Messages) {
			t.Errorf("event %d request messages = %#v, want %#v", i, e.Request.Messages, res.Steps[i].Request.Messages)
		}
	}
	if _, ok := events[0].Content[1].(types.ToolResultContent); !ok {
		t.Errorf("step 0 content = %#v, want a tool result second", events[0].Content)
	}
	if len(end.Request.Messages) == 0 || !reflect.DeepEqual(end.Request.Messages, res.Steps[1].Request.Messages) {
		t.Errorf("onEnd request messages = %#v, want the final step's", end.Request.Messages)
	}
}

// r72 gen fix 1 (survivor test): repair's InputSchema returns a bare
// *schema.JSONSchemaValidator's schema (TS repair inputSchema), not
// {"type":"object"}.
func TestR72Integ_RepairInputSchemaForBareValidator(t *testing.T) {
	want := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}}
	var got map[string]interface{}
	call, err := ParseToolCall(context.Background(), ParseToolCallOptions{
		ToolCall: types.ToolCall{ID: "call-1", ToolName: "tool3", RawArguments: `{}`},
		Tools:    []types.Tool{{Name: "tool3", Parameters: schema.NewJSONSchema(want)}},
		RepairToolCall: func(_ context.Context, o ToolCallRepairOptions) (*types.ToolCall, error) {
			got, _ = o.InputSchema("tool3")
			return nil, nil
		},
	})
	if err == nil && !call.Invalid {
		t.Fatal("invalid input accepted")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("repair input schema = %#v, want %#v", got, want)
	}
}

// r72 gen CR-1 (survivor test): every Output's parse failure has a
// JSONParseError cause (TS NoObjectGeneratedError.cause from safeParseJSON).
func TestR72Integ_OutputParseFailuresAreJSONParseErrors(t *testing.T) {
	ctx := context.Background()
	opts := ParseCompleteOutputOptions{Text: `{"broken"`}
	check := func(name string, err error) {
		t.Helper()
		var nog *NoObjectGeneratedError
		if !errors.As(err, &nog) || !providererrors.IsJSONParseError(nog.Cause) {
			t.Errorf("%s: err = %v (cause %T), want a JSONParseError cause", name, err, nog)
			return
		}
		var jpe *providererrors.JSONParseError
		if errors.As(nog.Cause, &jpe); jpe.Text != opts.Text {
			t.Errorf("%s: JSONParseError text = %q", name, jpe.Text)
		}
	}
	_, err := ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})}).ParseCompleteOutput(ctx, opts)
	check("object", err)
	_, err = ArrayOutput[string](ArrayOutputOptions[string]{ElementSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "string"})}).ParseCompleteOutput(ctx, opts)
	check("array", err)
	_, err = ChoiceOutput[string](ChoiceOutputOptions[string]{Options: []string{"a", "b"}}).ParseCompleteOutput(ctx, opts)
	check("choice", err)
	_, err = JSONOutput(JSONOutputOptions{}).ParseCompleteOutput(ctx, opts)
	check("json", err)
}

// r72 stream fix 2 (survivor test): an approved result whose approval
// request the source already sent maps to the output chunk only, and a
// locally executed output gets no providerExecuted flag even when the raw
// result marks it set (TS tool-result → tool-output-available).
func TestR72Integ_UIApprovedResultAfterRequest(t *testing.T) {
	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "v"}}},
		{Type: provider.ChunkTypeToolApprovalRequest, ToolApprovalRequest: &types.ToolApprovalRequestContent{ApprovalID: "a1", ToolCallID: "call-1"}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "tool1", Result: "ok",
			ApprovalStatus: types.ToolApprovalStatusApproved, ApprovalID: "a1", ProviderExecutedSet: true}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop"},
	})
	chunks, errCh := ToUIMessageStream(context.Background(), src)
	var requests int
	var output UIMessageChunk
	for c := range chunks {
		switch c["type"] {
		case "tool-approval-request":
			requests++
		case "tool-output-available":
			output = c
		}
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Errorf("approval requests = %d, want 1", requests)
	}
	if output == nil || output["output"] != "ok" {
		t.Fatalf("output chunk = %#v", output)
	}
	if _, ok := output["providerExecuted"]; ok {
		t.Errorf("output chunk = %#v, want no providerExecuted", output)
	}
}

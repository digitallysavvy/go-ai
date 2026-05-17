package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func TestEffectiveRuntimeContext(t *testing.T) {
	if got := effectiveRuntimeContext("runtime", "experimental"); got != "runtime" {
		t.Fatalf("expected runtime context preference, got %v", got)
	}
	if got := effectiveRuntimeContext(nil, "experimental"); got != "experimental" {
		t.Fatalf("expected experimental fallback, got %v", got)
	}
}

func TestNormalizeToolApprovalResult(t *testing.T) {
	reason := "manual"

	tests := []struct {
		name   string
		input  interface{}
		status types.ToolApprovalStatus
		reason *string
	}{
		{name: "nil", input: nil, status: types.ToolApprovalStatusNotApplicable},
		{name: "empty result", input: types.ToolApprovalResult{}, status: types.ToolApprovalStatusNotApplicable},
		{name: "pointer empty status", input: &types.ToolApprovalResult{}, status: types.ToolApprovalStatusNotApplicable},
		{name: "status type", input: types.ToolApprovalStatusApproved, status: types.ToolApprovalStatusApproved},
		{name: "status string", input: "denied", status: types.ToolApprovalStatusDenied},
		{name: "result with reason", input: &types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied, Reason: &reason}, status: types.ToolApprovalStatusDenied, reason: &reason},
		{name: "unknown type", input: 123, status: types.ToolApprovalStatusNotApplicable},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeToolApprovalResult(tc.input)
			if got.Status != tc.status {
				t.Fatalf("status = %q, want %q", got.Status, tc.status)
			}
			if tc.reason != nil {
				if got.Reason == nil || *got.Reason != *tc.reason {
					t.Fatalf("reason = %+v, want %q", got.Reason, *tc.reason)
				}
			}
		})
	}
}

func TestResolveToolApproval_CallLevelDispatch(t *testing.T) {
	call := types.ToolCall{ID: "call-1", ToolName: "search", Arguments: map[string]interface{}{"q": "go"}}
	tools := []types.Tool{{Name: "search"}}
	messages := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hello"}}}}
	runtimeCtx := map[string]interface{}{"tenant": "acme"}
	toolsCtx := map[string]interface{}{"search": map[string]interface{}{"scope": "private"}}

	t.Run("generic function", func(t *testing.T) {
		got := mustResolveToolApproval(t, context.Background(), call, tools, messages, runtimeCtx, toolsCtx, types.GenericToolApprovalFunc(func(opts types.ToolApprovalOptions) types.ToolApprovalResult {
			if opts.ToolCall.ID != "call-1" || opts.ToolsContext["search"] == nil || opts.RuntimeContext == nil || len(opts.Messages) != 1 {
				t.Fatalf("unexpected options: %+v", opts)
			}
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
		}))
		if got.Status != types.ToolApprovalStatusApproved {
			t.Fatalf("status = %q, want approved", got.Status)
		}
	})

	t.Run("deprecated function", func(t *testing.T) {
		got := mustResolveToolApproval(t, context.Background(), call, tools, messages, runtimeCtx, toolsCtx, types.ToolApprovalFunc(func(toolCall types.ToolCall, _ []types.Tool, _ []types.Message, _ interface{}, _ map[string]interface{}) types.ToolApprovalResult {
			if toolCall.ID != "call-1" {
				t.Fatalf("unexpected call: %+v", toolCall)
			}
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied}
		}))
		if got.Status != types.ToolApprovalStatusDenied {
			t.Fatalf("status = %q, want denied", got.Status)
		}
	})

	t.Run("map interface single-tool func", func(t *testing.T) {
		got := mustResolveToolApproval(t, context.Background(), call, tools, messages, runtimeCtx, toolsCtx, map[string]interface{}{
			"search": types.SingleToolApprovalFunc(func(args map[string]interface{}, opts types.SingleToolApprovalOptions) types.ToolApprovalResult {
				if args["q"] != "go" || opts.ToolCallID != "call-1" {
					t.Fatalf("unexpected args/options: args=%+v opts=%+v", args, opts)
				}
				if opts.ToolContext == nil || opts.RuntimeContext == nil || len(opts.Messages) != 1 {
					t.Fatalf("unexpected tool/runtime/messages: %+v", opts)
				}
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
			}),
		})
		if got.Status != types.ToolApprovalStatusApproved {
			t.Fatalf("status = %q, want approved", got.Status)
		}
	})

	t.Run("map interface static string", func(t *testing.T) {
		got := mustResolveToolApproval(t, context.Background(), call, tools, messages, runtimeCtx, toolsCtx, map[string]interface{}{
			"search": "user-approval",
		})
		if got.Status != types.ToolApprovalStatusUserApproval {
			t.Fatalf("status = %q, want user-approval", got.Status)
		}
	})

	t.Run("map typed approval values", func(t *testing.T) {
		got := mustResolveToolApproval(t, context.Background(), call, tools, messages, runtimeCtx, toolsCtx, map[string]types.ToolApprovalValue{
			"search": types.ToolApprovalStatusDenied,
		})
		if got.Status != types.ToolApprovalStatusDenied {
			t.Fatalf("status = %q, want denied", got.Status)
		}
	})

	t.Run("single-tool func validates context before callback", func(t *testing.T) {
		called := false
		_, err := resolveToolApproval(context.Background(), call, []types.Tool{{
			Name:          "search",
			ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "required": []string{"allowed"}}),
		}}, messages, runtimeCtx, map[string]interface{}{"search": map[string]interface{}{}}, map[string]types.ToolApprovalValue{
			"search": types.SingleToolApprovalFunc(func(map[string]interface{}, types.SingleToolApprovalOptions) types.ToolApprovalResult {
				called = true
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
			}),
		})
		if err == nil {
			t.Fatal("expected context validation error")
		}
		if called {
			t.Fatal("single-tool approval callback should not run when context validation fails")
		}
	})
}

func mustResolveToolApproval(t *testing.T, ctx context.Context, call types.ToolCall, tools []types.Tool, messages []types.Message, runtimeCtx interface{}, toolsCtx map[string]interface{}, toolApproval interface{}) types.ToolApprovalResult {
	t.Helper()
	got, err := resolveToolApproval(ctx, call, tools, messages, runtimeCtx, toolsCtx, toolApproval)
	if err != nil {
		t.Fatalf("resolveToolApproval() error = %v", err)
	}
	return got
}

func TestResolveToolApproval_ToolNeedsApprovalFallback(t *testing.T) {
	call := types.ToolCall{ID: "call-1", ToolName: "search", Arguments: map[string]interface{}{"q": "go"}}
	messages := []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hello"}}}}
	runtimeCtx := "runtime"

	t.Run("tool not found", func(t *testing.T) {
		got := mustResolveToolApproval(t, context.Background(), call, nil, messages, runtimeCtx, nil, nil)
		if got.Status != types.ToolApprovalStatusNotApplicable {
			t.Fatalf("status = %q, want not-applicable", got.Status)
		}
	})

	t.Run("bool true", func(t *testing.T) {
		got := mustResolveToolApproval(t, context.Background(), call, []types.Tool{{Name: "search", ToolApproval: true}}, messages, runtimeCtx, nil, nil)
		if got.Status != types.ToolApprovalStatusUserApproval {
			t.Fatalf("status = %q, want user-approval", got.Status)
		}
	})

	t.Run("bool false", func(t *testing.T) {
		got := mustResolveToolApproval(t, context.Background(), call, []types.Tool{{Name: "search", NeedsApproval: false}}, messages, runtimeCtx, nil, nil)
		if got.Status != types.ToolApprovalStatusNotApplicable {
			t.Fatalf("status = %q, want not-applicable", got.Status)
		}
	})

	t.Run("status constant", func(t *testing.T) {
		got := mustResolveToolApproval(t, context.Background(), call, []types.Tool{{Name: "search", ToolApproval: types.ToolApprovalStatusApproved}}, messages, runtimeCtx, nil, nil)
		if got.Status != types.ToolApprovalStatusApproved {
			t.Fatalf("status = %q, want approved", got.Status)
		}
	})

	t.Run("status string", func(t *testing.T) {
		got := mustResolveToolApproval(t, context.Background(), call, []types.Tool{{Name: "search", ToolApproval: "denied"}}, messages, runtimeCtx, nil, nil)
		if got.Status != types.ToolApprovalStatusDenied {
			t.Fatalf("status = %q, want denied", got.Status)
		}
	})

	t.Run("needs approval func true", func(t *testing.T) {
		got := mustResolveToolApproval(t, context.Background(), call, []types.Tool{{
			Name: "search",
			ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{
				"type":     "object",
				"required": []string{"scope", "region"},
				"properties": map[string]interface{}{
					"scope":  map[string]interface{}{"type": "string"},
					"region": map[string]interface{}{"type": "string", "default": "us-east-1"},
				},
			}),
			ToolApproval: types.ToolNeedsApprovalFunc(func(ctx context.Context, input map[string]interface{}, opts types.ToolNeedsApprovalOptions) bool {
				toolCtx := opts.Context.(map[string]interface{})
				return ctx != nil &&
					input["q"] == "go" &&
					opts.ToolCallID == "call-1" &&
					len(opts.Messages) == 1 &&
					toolCtx["scope"] == "private" &&
					toolCtx["region"] == "us-east-1"
			}),
		}}, messages, runtimeCtx, map[string]interface{}{"search": map[string]interface{}{"scope": "private"}}, nil)
		if got.Status != types.ToolApprovalStatusUserApproval {
			t.Fatalf("status = %q, want user-approval", got.Status)
		}
	})

	t.Run("deprecated needs approval func true", func(t *testing.T) {
		got := mustResolveToolApproval(t, context.Background(), call, []types.Tool{{
			Name: "search",
			ToolApproval: types.NeedsApprovalFunc(func(ctx context.Context, input map[string]interface{}) bool {
				return ctx != nil && input["q"] == "go"
			}),
		}}, messages, runtimeCtx, nil, nil)
		if got.Status != types.ToolApprovalStatusUserApproval {
			t.Fatalf("status = %q, want user-approval", got.Status)
		}
	})

	t.Run("needs approval func with invalid context schema", func(t *testing.T) {
		called := false
		_, err := resolveToolApproval(context.Background(), call, []types.Tool{{
			Name:          "search",
			ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "required": []string{"allowed"}}),
			ToolApproval: types.ToolNeedsApprovalFunc(func(context.Context, map[string]interface{}, types.ToolNeedsApprovalOptions) bool {
				called = true
				return false
			}),
		}}, messages, runtimeCtx, map[string]interface{}{"search": map[string]interface{}{}}, nil)
		if err == nil {
			t.Fatal("expected context validation error")
		}
		if called {
			t.Fatal("approval callback should not run when context validation fails")
		}
	})

	t.Run("deprecated needs approval alias fallback", func(t *testing.T) {
		got := mustResolveToolApproval(t, context.Background(), call, []types.Tool{{Name: "search", NeedsApproval: true}}, messages, runtimeCtx, nil, nil)
		if got.Status != types.ToolApprovalStatusUserApproval {
			t.Fatalf("status = %q, want user-approval", got.Status)
		}
	})
}

func TestValidateToolContextForAndMergeProviderMetadataMaps(t *testing.T) {
	s := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "required": []string{"ok"}})
	tool := &types.Tool{Name: "search", ContextSchema: s}

	if _, err := validateToolContextFor(tool, "search", map[string]interface{}{}); err == nil {
		t.Fatal("expected context validation error")
	} else {
		var validationErr *providererrors.ValidationError
		if !errors.As(err, &validationErr) {
			t.Fatalf("expected validation error, got %T: %v", err, err)
		}
		if validationErr.Context == nil || validationErr.Context.Field != "tool context" || validationErr.Context.EntityName != "search" {
			t.Fatalf("validation context = %+v, want field tool context and entity search", validationErr.Context)
		}
	}

	ctxValue, err := validateToolContextFor(tool, "search", map[string]interface{}{"ok": true})
	if err != nil || ctxValue == nil {
		t.Fatalf("expected valid context, got ctx=%v err=%v", ctxValue, err)
	}

	defaultSchema := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":     "object",
		"required": []string{"ok", "region"},
		"properties": map[string]interface{}{
			"ok":     map[string]interface{}{"type": "boolean"},
			"region": map[string]interface{}{"type": "string", "default": "us-east-1"},
		},
	})
	ctxValue, err = validateToolContextFor(&types.Tool{Name: "search", ContextSchema: defaultSchema}, "search", map[string]interface{}{"ok": true})
	if err != nil {
		t.Fatalf("expected defaulted context to validate: %v", err)
	}
	ctxMap, ok := ctxValue.(map[string]interface{})
	if !ok || ctxMap["region"] != "us-east-1" {
		t.Fatalf("context defaults = %+v, want region=us-east-1", ctxValue)
	}

	merged := mergeProviderMetadataMaps(map[string]interface{}{"a": 1}, map[string]interface{}{"a": 0, "b": 2})
	if merged["a"] != 1 || merged["b"] != 2 {
		t.Fatalf("unexpected merged metadata: %+v", merged)
	}
	if mergeProviderMetadataMaps(nil, nil) != nil {
		t.Fatal("expected nil merged metadata for empty inputs")
	}
}

func TestGenerateText_PerToolApprovalValidatesContextBeforeCallback(t *testing.T) {
	t.Parallel()

	called := false
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				ToolCalls:    []types.ToolCall{{ID: "call-1", ToolName: "search", Arguments: map[string]interface{}{"q": "go"}}},
				FinishReason: types.FinishReasonToolCalls,
			}, nil
		},
	}

	result, err := GenerateText(context.Background(), GenerateTextOptions{
		Model:  model,
		Prompt: "hi",
		Tools: []types.Tool{{
			Name:          "search",
			Parameters:    map[string]interface{}{"type": "object"},
			ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "required": []string{"allowed"}}),
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				t.Fatal("tool should not execute when context validation fails")
				return nil, nil
			},
		}},
		ToolsContext: map[string]interface{}{"search": map[string]interface{}{}},
		ToolApproval: map[string]types.ToolApprovalValue{
			"search": types.SingleToolApprovalFunc(func(map[string]interface{}, types.SingleToolApprovalOptions) types.ToolApprovalResult {
				called = true
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusApproved}
			}),
		},
		StopWhen: []StopCondition{StepCountIs(1)},
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if called {
		t.Fatal("approval callback should not run when context validation fails")
	}
	if len(result.ToolResults) == 0 || result.ToolResults[0].Error == nil {
		t.Fatalf("expected validation error tool result, got %+v", result.ToolResults)
	}
}

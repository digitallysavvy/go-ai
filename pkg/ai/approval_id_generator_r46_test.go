package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS generateText/streamText create approval ids with the call's generateId
// (_internal.generateId; InternalOptions.GenerateID here).
func TestApprovalIDsUseTheCallIDGenerator(t *testing.T) {
	t.Parallel()
	tool := types.Tool{
		Name:         "danger",
		Parameters:   map[string]interface{}{"type": "object"},
		ToolApproval: types.ToolApprovalStatusUserApproval,
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}
	call := types.ToolCall{ID: "c1", ToolName: "danger", Arguments: map[string]interface{}{}}
	approvalID := func(content []types.ContentPart) string {
		for _, part := range content {
			if req, ok := part.(types.ToolApprovalRequestContent); ok {
				return req.ApprovalID
			}
		}
		return ""
	}
	internal := func() *InternalOptions {
		return &InternalOptions{GenerateID: func() string { return "gen-id" }}
	}

	gen, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: &testutil.MockLanguageModel{
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{call}}, nil
			},
		},
		Prompt: "go", Tools: []types.Tool{tool}, Internal: internal(),
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if got := approvalID(gen.Content); got != "gen-id" {
		t.Errorf("GenerateText approval id = %q, want the generated id", got)
	}

	stream, err := StreamText(context.Background(), StreamTextOptions{
		Model: &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &call},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			},
		},
		Prompt: "go", Tools: []types.Tool{tool}, Internal: internal(),
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := stream.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if got := approvalID(stream.Steps()[0].Content); got != "gen-id" {
		t.Errorf("StreamText approval id = %q, want the generated id", got)
	}
}

// TS default generateId: createIdGenerator({prefix: 'aitxt', size: 24}) for
// generateText/streamText and {prefix: 'aiobj'} for generateObject/
// streamObject; Go used the callId generator ("call-").
func TestDefaultGeneratedIDPrefixesLikeTS(t *testing.T) {
	t.Parallel()
	check := func(what, id, prefix string) {
		t.Helper()
		if !strings.HasPrefix(id, prefix+"-") || len(id) != len(prefix)+1+24 {
			t.Errorf("%s id = %q, want %s- plus 24 characters", what, id, prefix)
		}
	}
	check("text", newTextID(), "aitxt")
	check("object", newObjectID(), "aiobj")
	check("call", newCallID(), "call")

	gen, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: &testutil.MockLanguageModel{
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop}, nil
			},
		},
		Prompt: "hi",
	})
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	check("GenerateText response", gen.Response.ID, "aitxt")

	stream, err := StreamText(context.Background(), StreamTextOptions{
		Model: &testutil.MockLanguageModel{
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "hi"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			},
		},
		Prompt: "hi",
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := stream.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	check("StreamText response", stream.Response().ID, "aitxt")
}

// TS generateObject/streamObject default generateId is
// createIdGenerator({prefix: 'aiobj', size: 24}): a response without an id
// gets an aiobj- id.
func TestDefaultObjectResponseIDPrefixLikeTS(t *testing.T) {
	t.Parallel()
	objSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})
	want := func(name, id string) {
		if !strings.HasPrefix(id, "aiobj-") || len(id) != len("aiobj-")+24 {
			t.Errorf("%s response id = %q, want aiobj- plus 24 characters", name, id)
		}
	}
	gen, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model: &testutil.MockLanguageModel{
			StructuredSupport: true,
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{Text: `{"a":1}`, FinishReason: types.FinishReasonStop}, nil
			},
		},
		Prompt: "hi",
		Schema: objSchema,
	})
	if err != nil {
		t.Fatalf("GenerateObject() error = %v", err)
	}
	want("GenerateObject", gen.Response.ID)

	stream, err := StreamObject(context.Background(), StreamObjectOptions{
		Model: &testutil.MockLanguageModel{
			StructuredSupport: true,
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: `{"a":1}`},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			},
		},
		Prompt: "hi",
		Schema: objSchema,
	})
	if err != nil {
		t.Fatalf("StreamObject() error = %v", err)
	}
	want("StreamObject", stream.Response.ID)
}

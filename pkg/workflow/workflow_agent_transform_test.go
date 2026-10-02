package workflow

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestWorkflowStreamAppliesExperimentalTransformBeforeOnChunk mirrors TS's
// workflow-agent-transform.test.ts ("transforms provider stream parts
// before writing them", hash 165455d): WorkflowAgent declared an
// experimental_transform stream option but never forwarded it to the
// underlying model call, so configured transforms never ran and oversized
// provider stream parts reached consumers unchanged. This proves
// WorkflowAgent.StreamWithOptions's ExperimentalTransform option actually
// reaches OnChunk: a transform rewrites an oversized provider-executed
// tool result before it is observed by the caller.
func TestWorkflowStreamAppliesExperimentalTransformBeforeOnChunk(t *testing.T) {
	oversizedResult := map[string]interface{}{"data": strings.Repeat("x", 2048)}
	omittedResult := map[string]interface{}{"reason": "too_large_for_stream"}

	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{
					Type: provider.ChunkTypeToolCall,
					ToolCall: &types.ToolCall{
						ID:               "provider-call",
						ToolName:         "providerTool",
						Arguments:        map[string]interface{}{},
						ProviderExecuted: true,
					},
				},
				{
					Type: provider.ChunkTypeToolResult,
					ToolResult: &types.ToolResult{
						ToolCallID: "provider-call",
						ToolName:   "providerTool",
						Result:     oversizedResult,
					},
				},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}

	wfAgent, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: []types.Tool{{Name: "providerTool", ProviderExecuted: true}},
	})
	if err != nil {
		t.Fatalf("NewWorkflowAgent() error = %v", err)
	}

	var transformSawOversizedResult bool
	transform := func(_ context.Context, chunk provider.StreamChunk) []provider.StreamChunk {
		if chunk.Type == provider.ChunkTypeToolResult && chunk.ToolResult != nil {
			if m, ok := chunk.ToolResult.Result.(map[string]interface{}); ok {
				if _, big := m["data"]; big {
					transformSawOversizedResult = true
					replaced := *chunk.ToolResult
					replaced.Result = omittedResult
					chunk.ToolResult = &replaced
				}
			}
		}
		return []provider.StreamChunk{chunk}
	}

	var mu sync.Mutex
	var sawOmittedResult bool
	var sawOversizedResult bool

	res, err := wfAgent.StreamWithOptions(context.Background(), WorkflowStreamOptions{
		Prompt:                "Run the provider tool.",
		ExperimentalTransform: []ai.StreamTransformFunc{transform},
		OnChunk: func(c provider.StreamChunk) {
			mu.Lock()
			defer mu.Unlock()
			if c.Type != provider.ChunkTypeToolResult || c.ToolResult == nil {
				return
			}
			m, ok := c.ToolResult.Result.(map[string]interface{})
			if !ok {
				return
			}
			if _, ok := m["reason"]; ok {
				sawOmittedResult = true
			}
			if _, ok := m["data"]; ok {
				sawOversizedResult = true
			}
		},
	})
	if err != nil {
		t.Fatalf("StreamWithOptions() error = %v", err)
	}

	for {
		_, e := res.Stream().Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatalf("stream next err: %v", e)
		}
	}

	if !transformSawOversizedResult {
		t.Fatal("expected the transform to see the oversized provider tool result")
	}

	mu.Lock()
	defer mu.Unlock()
	if !sawOmittedResult {
		t.Fatal("expected OnChunk to observe the transform's replacement result")
	}
	if sawOversizedResult {
		t.Fatal("expected OnChunk to never observe the untransformed oversized result")
	}
}

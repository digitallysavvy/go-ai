package harness

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestNormalizeToolModelOutput(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	out, err := NormalizeToolModelOutput(types.ToolResultOutput{
		Type: types.ToolResultOutputContent,
		Content: []types.ToolResultContentBlock{
			types.TextContentBlock{Text: "marker"},
			types.ImageContentBlock{Data: png, MediaType: "image/*"},
			types.FileContentBlock{Data: png, MediaType: "image/png"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if img, ok := out.Content[1].(types.ImageContentBlock); !ok || img.MediaType != "image/png" {
		t.Fatalf("detected image = %+v", out.Content[1])
	}
	if img, ok := out.Content[2].(types.ImageContentBlock); !ok || img.MediaType != "image/png" {
		t.Fatalf("file image = %+v", out.Content[2])
	}

	for name, block := range map[string]types.ToolResultContentBlock{
		"url file": types.FileContentBlock{URL: "https://x/y.png", MediaType: "image/png"},
		"pdf":      types.FileContentBlock{Data: []byte("x"), MediaType: "application/pdf"},
		"undetect": types.ImageContentBlock{Data: []byte("zz"), MediaType: "image/*"},
		"custom":   types.CustomContentBlock{},
	} {
		_, err := NormalizeToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{block}})
		if !IsCapabilityUnsupportedError(err) {
			t.Fatalf("%s: err = %v, want capability-unsupported", name, err)
		}
	}
	passthrough, err := NormalizeToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: 1})
	if err != nil || passthrough.Value != 1 {
		t.Fatalf("passthrough = %+v, %v", passthrough, err)
	}
}

func TestConvertToolModelOutput(t *testing.T) {
	content, isErr, err := ConvertToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputErrorJSON, Value: map[string]any{"failed": true}})
	if err != nil || !isErr || len(content) != 1 || content[0].Text != `{"failed":true}` {
		t.Fatalf("error-json = %+v %v %v", content, isErr, err)
	}
	content, isErr, _ = ConvertToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputExecutionDenied})
	if !isErr || content[0].Text != "Tool execution denied." {
		t.Fatalf("denied = %+v", content)
	}
	content, _, err = ConvertToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
		types.TextContentBlock{Text: "marker"},
		types.ImageContentBlock{Data: []byte{1, 2, 3}, MediaType: "image/png"},
	}})
	if err != nil || len(content) != 2 || content[1].Type != ToolModelOutputContentImage || content[1].Data != "AQID" {
		t.Fatalf("content = %+v %v", content, err)
	}
	if _, _, err := ConvertToolModelOutput(types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{types.FileContentBlock{URL: "u"}}}); !IsCapabilityUnsupportedError(err) {
		t.Fatalf("err = %v", err)
	}
}

// Ports TS #22367: a host tool's toModelOutput runs once per submitted
// result; the submission carries the model-facing output, the raw result is
// preserved on the tool result, and ResponseMessages reports the model
// output.
func TestRunPrompt_ToModelOutputRunsOnceAndPreservesRaw(t *testing.T) {
	var executed, converted atomic.Int32
	modelOutput := types.ToolResultOutput{
		Type: types.ToolResultOutputContent,
		Content: []types.ToolResultContentBlock{
			types.TextContentBlock{Text: "marker"},
			types.ImageContentBlock{Data: []byte{1, 2, 3}, MediaType: "image/png"},
		},
	}
	inspect := types.Tool{
		Name: "inspect", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			executed.Add(1)
			return map[string]any{"status": "ready"}, nil
		},
		ToModelOutput: func(ctx context.Context, o types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
			converted.Add(1)
			return &modelOutput, nil
		},
	}
	stop := FinishReason{Unified: FinishReasonStop}
	mock := newScriptedHarness(
		func(emit func(StreamPart)) {
			emit(&StreamStartPart{})
			emit(&ToolCallPart{ToolCallID: "call", ToolName: "inspect", Input: "{}"})
		},
		func(sub ToolResultSubmission, emit func(StreamPart)) {
			emit(&ToolResultPart{ToolCallID: "call", ToolName: "inspect", Result: map[string]any{"status": "ready"}})
			emit(&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)})
			emit(&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)})
			emit(&FinishPart{FinishReason: stop, TotalUsage: stringUsage(2, 2)})
		},
	)
	tools := map[string]types.Tool{"inspect": inspect}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("Inspect"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
	})
	chunks := drainRunPrompt(t, out)

	if executed.Load() != 1 || converted.Load() != 1 {
		t.Fatalf("executed=%d converted=%d, want 1/1", executed.Load(), converted.Load())
	}
	if len(mock.toolResults) != 1 {
		t.Fatalf("submissions = %+v", mock.toolResults)
	}
	mtr, ok := mock.toolResults[0].ToolResult.(*ModelToolResult)
	if !ok || mtr.Type != "tool-result" || mtr.ToolName != "inspect" || len(mtr.Output.Content) != 2 {
		t.Fatalf("submission.ToolResult = %+v", mock.toolResults[0].ToolResult)
	}
	if m, _ := mock.toolResults[0].Output.(map[string]any); m["status"] != "ready" {
		t.Fatalf("raw output = %+v", mock.toolResults[0].Output)
	}
	results := chunksOfType(chunks, provider.ChunkTypeToolResult)
	if len(results) != 1 || results[0].ToolResult.Result == nil {
		t.Fatalf("tool results = %+v", results)
	}
	var toolMsg *types.Message
	for _, m := range out.Result.ResponseMessages() {
		m := m
		if m.Role == types.RoleTool {
			toolMsg = &m
		}
	}
	if toolMsg == nil {
		t.Fatal("no tool response message")
	}
	part, ok := toolMsg.Content[0].(types.ToolResultContent)
	if !ok || part.Output == nil || part.Output.Type != types.ToolResultOutputContent || len(part.Output.Content) != 2 {
		t.Fatalf("tool message part = %+v output=%+v", toolMsg.Content[0], part.Output)
	}
}

func TestRunPrompt_ToModelOutputErrorSubmitsError(t *testing.T) {
	inspect := types.Tool{
		Name: "inspect", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
		ToModelOutput: func(ctx context.Context, o types.ToModelOutputOptions) (*types.ToolResultOutput, error) {
			return nil, errors.New("convert failed")
		},
	}
	mock := newMockHarness(mockHarnessOptions{
		script: func(submit func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&ToolCallPart{ToolCallID: "call", ToolName: "inspect", Input: "{}"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	tools := map[string]types.Tool{"inspect": inspect}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("x"), SandboxSession: testSandbox(),
		Tools: tools, ActiveTools: tools,
	})
	drainRunPrompt(t, out)
	if len(mock.toolResults) != 1 || !mock.toolResults[0].IsError {
		t.Fatalf("submissions = %+v, want one error", mock.toolResults)
	}
}

// A client-supplied continuation whose model output is unsupported falls back
// to the JSON text of the raw output instead of failing the turn (TS #22367).
func TestRunPrompt_ContinuationUnsupportedModelOutputFallsBackToText(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		continueScript: func(submit func(string, interface{})) []StreamPart {
			stop := FinishReason{Unified: FinishReasonStop}
			return []StreamPart{
				&StreamStartPart{},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	pdf := types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
		types.FileContentBlock{Data: []byte("x"), MediaType: "application/pdf"},
	}}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: mock.harness, Session: mock.session, Mode: "continue", SandboxSession: testSandbox(),
		PendingToolResults:      []PendingToolResult{{ToolCallID: "c1", ToolName: "clientTool", Input: "{}"}},
		ToolResultContinuations: []types.ToolResultContent{{ToolCallID: "c1", ToolName: "clientTool", Output: &pdf}},
	})
	drainRunPrompt(t, out)
	if err := out.Result.Err(); err != nil {
		t.Fatalf("turn failed: %v", err)
	}
	if len(mock.toolResults) != 1 {
		t.Fatalf("submissions = %+v", mock.toolResults)
	}
	tr, ok := mock.toolResults[0].ToolResult.(*types.ToolResultContent)
	if !ok || tr.Output == nil || tr.Output.Type != types.ToolResultOutputText {
		t.Fatalf("ToolResult = %+v, want text fallback", mock.toolResults[0].ToolResult)
	}
}

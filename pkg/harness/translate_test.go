package harness

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ports TS internal/translate-stream-part.test.ts.

func TestTranslatePart_ToolCallReturnsNothing(t *testing.T) {
	// Validation is handled by run_prompt.go, not translate.go.
	chunks := TranslatePart(&ToolCallPart{ToolCallID: "c1", ToolName: "bash", Input: "{}"}, TranslateOptions{})
	if len(chunks) != 0 {
		t.Fatalf("chunks = %+v, want none", chunks)
	}
}

func TestTranslatePart_PreservesDynamicOnToolResult(t *testing.T) {
	chunks := TranslatePart(&ToolResultPart{ToolCallID: "c1", ToolName: "bash", Result: "ok", Dynamic: true}, TranslateOptions{})
	if len(chunks) != 1 || chunks[0].Type != provider.ChunkTypeToolResult {
		t.Fatalf("chunks = %+v", chunks)
	}
	if !chunks[0].ToolResult.Dynamic {
		t.Fatalf("Dynamic = false, want true")
	}
	if chunks[0].ToolResult.Result != "ok" {
		t.Fatalf("Result = %v", chunks[0].ToolResult.Result)
	}
}

func TestTranslatePart_FailedProviderExecutedToolResultBecomesError(t *testing.T) {
	part := &ToolResultPart{ToolCallID: "c1", ToolName: "bash", Result: "boom", IsError: true}
	chunks := TranslatePart(part, TranslateOptions{IsProviderExecuted: func(string) bool { return true }})
	if len(chunks) != 1 {
		t.Fatalf("chunks = %+v", chunks)
	}
	tr := chunks[0].ToolResult
	if tr == nil || tr.Error == nil {
		t.Fatalf("expected ToolResult.Error to be set, got %+v", tr)
	}
	if tr.Error.Error() != "boom" {
		t.Fatalf("Error = %q, want boom", tr.Error.Error())
	}
	if !tr.ProviderExecuted {
		t.Fatalf("ProviderExecuted = false, want true")
	}
}

func TestTranslatePart_FailedHostToolResultStaysToolResult(t *testing.T) {
	part := &ToolResultPart{ToolCallID: "c1", ToolName: "myTool", Result: "boom", IsError: true}
	// isProviderExecuted=false: a host tool's own failure is not turned into
	// tool-error here (run_prompt.go's own host-execution path handles that).
	chunks := TranslatePart(part, TranslateOptions{IsProviderExecuted: func(string) bool { return false }})
	if len(chunks) != 1 {
		t.Fatalf("chunks = %+v", chunks)
	}
	tr := chunks[0].ToolResult
	if tr == nil || tr.Error != nil {
		t.Fatalf("expected no Error, got %+v", tr)
	}
	if tr.Result != "boom" {
		t.Fatalf("Result = %v, want boom", tr.Result)
	}
}

func TestTranslatePart_PreservesDynamicOnFailedToolResult(t *testing.T) {
	part := &ToolResultPart{ToolCallID: "c1", ToolName: "bash", Result: "boom", IsError: true, Dynamic: true}
	chunks := TranslatePart(part, TranslateOptions{IsProviderExecuted: func(string) bool { return true }})
	if len(chunks) != 1 || !chunks[0].ToolResult.Dynamic {
		t.Fatalf("chunks = %+v", chunks)
	}
}

func TestTranslatePart_FileChangeFansOutToToolCallAndResult(t *testing.T) {
	chunks := TranslatePart(&FileChangePart{Event: FileChangeModify, Path: "/work/a.ts"}, TranslateOptions{})
	if len(chunks) != 2 {
		t.Fatalf("chunks = %+v, want 2", chunks)
	}
	if chunks[0].Type != provider.ChunkTypeToolCall || chunks[0].ToolCall.ToolName != "fileChange" {
		t.Fatalf("chunks[0] = %+v", chunks[0])
	}
	if !chunks[0].ToolCall.Dynamic || !chunks[0].ToolCall.ProviderExecuted {
		t.Fatalf("chunks[0].ToolCall = %+v, want dynamic+providerExecuted", chunks[0].ToolCall)
	}
	if chunks[1].Type != provider.ChunkTypeToolResult || chunks[1].ToolResult.ToolCallID != chunks[0].ToolCall.ID {
		t.Fatalf("chunks[1] = %+v", chunks[1])
	}
}

func TestTranslatePart_CompactionFansOutWithMetadataOutput(t *testing.T) {
	before, after := 100.0, 40.0
	chunks := TranslatePart(&CompactionPart{Trigger: CompactionTriggerAuto, Summary: "s", TokensBefore: &before, TokensAfter: &after}, TranslateOptions{})
	if len(chunks) != 2 {
		t.Fatalf("chunks = %+v, want 2", chunks)
	}
	if chunks[0].ToolCall.ToolName != "compaction" || len(chunks[0].ToolCall.Arguments) != 0 {
		t.Fatalf("chunks[0].ToolCall = %+v, want empty input", chunks[0].ToolCall)
	}
	output := chunks[1].ToolResult.Result.(map[string]interface{})
	if output["trigger"] != CompactionTriggerAuto || output["summary"] != "s" {
		t.Fatalf("output = %+v", output)
	}
	if output["tokensBefore"] != before || output["tokensAfter"] != after {
		t.Fatalf("output tokens = %+v", output)
	}
}

func TestTranslatePart_CompactionOmitsOptionalTokenFields(t *testing.T) {
	chunks := TranslatePart(&CompactionPart{Trigger: CompactionTriggerManual, Summary: "s"}, TranslateOptions{})
	output := chunks[1].ToolResult.Result.(map[string]interface{})
	if _, ok := output["tokensBefore"]; ok {
		t.Fatalf("tokensBefore should be absent: %+v", output)
	}
	if _, ok := output["tokensAfter"]; ok {
		t.Fatalf("tokensAfter should be absent: %+v", output)
	}
}

func TestTranslatePart_ReturnsNothingForInternalParts(t *testing.T) {
	internal := []StreamPart{
		&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop}},
		&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}},
	}
	for _, p := range internal {
		if chunks := TranslatePart(p, TranslateOptions{}); len(chunks) != 0 {
			t.Fatalf("%T: chunks = %+v, want none", p, chunks)
		}
	}
	// stream-start with no warnings also produces nothing.
	if chunks := TranslatePart(&StreamStartPart{}, TranslateOptions{}); len(chunks) != 0 {
		t.Fatalf("StreamStartPart with no warnings: chunks = %+v, want none", chunks)
	}
}

func TestTranslatePart_ForwardsStreamingToolInputEvents(t *testing.T) {
	start := TranslatePart(&ToolInputStartPart{ID: "c1", ToolName: "bash"}, TranslateOptions{})
	if len(start) != 1 || start[0].Type != provider.ChunkTypeToolInputStart || start[0].ToolCall.ID != "c1" {
		t.Fatalf("start = %+v", start)
	}
	delta := TranslatePart(&ToolInputDeltaPart{ID: "c1", Delta: `{"cmd":`}, TranslateOptions{})
	if len(delta) != 1 || delta[0].Type != provider.ChunkTypeToolInputDelta || delta[0].ID != "c1" || delta[0].Text != `{"cmd":` {
		t.Fatalf("delta = %+v", delta)
	}
	end := TranslatePart(&ToolInputEndPart{ID: "c1"}, TranslateOptions{})
	if len(end) != 1 || end[0].Type != provider.ChunkTypeToolInputEnd || end[0].ToolCall.ID != "c1" {
		t.Fatalf("end = %+v", end)
	}
}

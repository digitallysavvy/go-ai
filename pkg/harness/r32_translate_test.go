package harness

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS passes tool-input-delta / tool-input-end through unchanged, including
// providerMetadata.
func TestTranslateToolInputDeltaAndEndKeepProviderMetadata(t *testing.T) {
	md := ProviderMetadata{"x": {"k": "v"}}
	for _, part := range []StreamPart{
		&ToolInputDeltaPart{ID: "1", Delta: "d", ProviderMetadata: md},
		&ToolInputEndPart{ID: "1", ProviderMetadata: md},
	} {
		chunks := TranslatePart(part, TranslateOptions{})
		if len(chunks) != 1 || string(chunks[0].ProviderMetadata) != `{"x":{"k":"v"}}` {
			t.Fatalf("%T: %+v", part, chunks)
		}
	}
	if end := TranslatePart(&ToolInputEndPart{ID: "7"}, TranslateOptions{}); end[0].ID != "7" || end[0].Type != provider.ChunkTypeToolInputEnd {
		t.Fatalf("end chunk id lost: %+v", end[0])
	}
}

// The synthetic fileChange / compaction results carry `input`, and a failed
// provider-executed result becomes a tool-error without `preliminary`.
func TestTranslateSyntheticResultInputAndToolErrorPreliminary(t *testing.T) {
	fc := TranslatePart(&FileChangePart{Event: FileChangeCreate, Path: "a"}, TranslateOptions{})
	if got := fc[1].ToolResult.Input; got["path"] != "a" || got["event"] != FileChangeCreate {
		t.Fatalf("fileChange input = %v", got)
	}
	cp := TranslatePart(&CompactionPart{Trigger: CompactionTriggerAuto, Summary: "s"}, TranslateOptions{})
	if cp[1].ToolResult.Input == nil || len(cp[1].ToolResult.Input) != 0 {
		t.Fatalf("compaction input = %v", cp[1].ToolResult.Input)
	}
	tr := TranslatePart(&ToolResultPart{ToolCallID: "c", ToolName: "t", IsError: true, Result: "boom", Preliminary: true}, TranslateOptions{})
	if tr[0].ToolResult.Error == nil || tr[0].ToolResult.Preliminary {
		t.Fatalf("tool-error = %+v", tr[0].ToolResult)
	}
}

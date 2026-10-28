package harness

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS HarnessStreamTextResult.finish(input) assigns the adapter finish's raw
// finish reason and provider metadata, also when it has none, so the result
// does not keep the last step's. finish() without input (no adapter finish)
// keeps them, and its finish part carries the last step's provider metadata
// and the summed step usage (totalUsage).
func TestR51TerminalFinishMetadataLikeTS(t *testing.T) {
	for _, adapterFinish := range []bool{true, false} {
		mock := newMockHarness(mockHarnessOptions{
			script: func(submit func(string, interface{})) []StreamPart {
				parts := []StreamPart{
					&StreamStartPart{},
					&TextStartPart{ID: "t1"}, &TextDeltaPart{ID: "t1", Delta: "hi"}, &TextEndPart{ID: "t1"},
					&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop, Raw: "end_turn"}, Usage: stringUsage(7, 2), HarnessMetadata: Metadata{"x": {"a": 1}}},
				}
				if adapterFinish {
					parts = append(parts, &FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop}, TotalUsage: stringUsage(9, 3)})
				}
				return parts
			},
		})
		out := runPrompt(context.Background(), runPromptInput{
			Harness: mock.harness, Session: mock.session, Prompt: TextPrompt("go"), SandboxSession: testSandbox(),
		})
		finishes := chunksOfType(drainRunPrompt(t, out), provider.ChunkTypeFinish)
		if len(finishes) != 1 {
			t.Fatalf("adapterFinish=%v: %d finish chunks", adapterFinish, len(finishes))
		}
		f := finishes[0]
		raw, md := out.Result.RawFinishReason(), string(out.Result.ProviderMetadata())
		if adapterFinish {
			if raw != "" || md != "" {
				t.Errorf("adapter finish: result raw = %q, metadata = %s; want none", raw, md)
			}
			continue
		}
		if raw != "end_turn" || md != `{"x":{"a":1}}` {
			t.Errorf("no adapter finish: result raw = %q, metadata = %s", raw, md)
		}
		if string(f.ProviderMetadata) != `{"x":{"a":1}}` || f.Usage == nil || f.Usage.InputTokens == nil || *f.Usage.InputTokens != 7 || *f.Usage.OutputTokens != 2 {
			t.Errorf("no adapter finish: finish part metadata = %s, usage = %+v", f.ProviderMetadata, f.Usage)
		}
	}
}

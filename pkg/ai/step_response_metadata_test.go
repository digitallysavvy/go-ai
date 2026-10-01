package ai

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TestStreamText_StepResponsePopulatesIDModelIDTimestamp verifies that
// StepResult.Response.ID/ModelID/Timestamp are populated from the stream's
// response-metadata chunk, not just Headers (audit: "From P0-3b: stream.go
// per-step StepResult.Response never sets ID/ModelID/Timestamp (only
// Headers)").
func TestStreamText_StepResponsePopulatesIDModelIDTimestamp(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	model := &testutil.MockLanguageModel{
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{
					ID:        "resp_123",
					ModelID:   "resolved-model",
					Timestamp: ts,
				}},
				{Type: provider.ChunkTypeText, Text: "hi"},
				{Type: provider.ChunkTypeFinish, FinishReason: "stop"},
			}), nil
		},
	}

	result, err := StreamText(context.Background(), StreamTextOptions{
		Model:  model,
		Prompt: "hi",
	})
	if err != nil {
		t.Fatalf("StreamText() error = %v", err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	steps := result.Steps()
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	resp := steps[0].Response
	if resp.ID != "resp_123" {
		t.Errorf("Response.ID = %q, want %q", resp.ID, "resp_123")
	}
	if resp.ModelID != "resolved-model" {
		t.Errorf("Response.ModelID = %q, want %q", resp.ModelID, "resolved-model")
	}
	if !resp.Timestamp.Equal(ts) {
		t.Errorf("Response.Timestamp = %v, want %v", resp.Timestamp, ts)
	}

	// Also verify Response() (used across the whole call) reflects the same.
	got := result.Response()
	if got.ID != "resp_123" || got.ModelID != "resolved-model" || !got.Timestamp.Equal(ts) {
		t.Errorf("Response() = %+v, want ID=resp_123 ModelID=resolved-model Timestamp=%v", got, ts)
	}
}

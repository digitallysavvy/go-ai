package ai

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS toUIMessageStream calls messageMetadata once per stream part. The
// StreamText full stream carries its own start part, so the start part is
// seen once, not again after the start chunk was built.
func TestR64UIMessageMetadataIsCalledOncePerPart(t *testing.T) {
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "ok"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
		}), nil
	}}
	res, err := StreamText(context.Background(), StreamTextOptions{Prompt: "a", Model: model})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var seen []string
	ch, errc := res.ToUIMessageStream(context.Background(), UIMessageStreamResultOptions{MessageMetadata: func(p map[string]interface{}) map[string]interface{} {
		mu.Lock()
		seen = append(seen, p["type"].(string))
		mu.Unlock()
		return nil
	}})
	for range ch {
	}
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := "start start-step text-start text-delta text-end finish-step finish"
	if got := strings.Join(seen, " "); got != want {
		t.Fatalf("messageMetadata parts = %q, want %q", got, want)
	}
}

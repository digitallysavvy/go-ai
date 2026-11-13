package ai

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ruling 4 (round 96, stream CR 2): the finish-chunk provider metadata on
// LanguageModelCallEndEvent reads 1e400 as +Inf (TS JSON.parse), instead of
// dropping the whole map.
func TestR96integLanguageModelCallEndMetadataInf(t *testing.T) {
	md := json.RawMessage(`{"p":{"x":1e400}}`)
	var mu sync.Mutex
	var got interface{}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeTextStart, ID: "t"},
			{Type: provider.ChunkTypeText, ID: "t", Text: "hi"},
			{Type: provider.ChunkTypeTextEnd, ID: "t"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage, ProviderMetadata: md},
		}),
		Prompt: "x",
		OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) {
			mu.Lock()
			defer mu.Unlock()
			if p, ok := e.ProviderMetadata["p"].(map[string]interface{}); ok {
				got = p["x"]
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if x, _ := got.(float64); !math.IsInf(x, 1) {
		t.Fatalf("OnLanguageModelCallEnd providerMetadata p.x = %#v, want +Inf", got)
	}
}

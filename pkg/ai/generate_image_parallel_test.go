package ai

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\n")

// TS runs the model calls in parallel and sums the token usage.
func TestGenerateImage_CallsRunInParallelAndSumTokenUsage(t *testing.T) {
	t.Parallel()
	var inflight, peak int32
	model := &testutil.MockImageModel{
		MaxImages: 1,
		DoGenerateFunc: func(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
			cur := atomic.AddInt32(&inflight, 1)
			for {
				p := atomic.LoadInt32(&peak)
				if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&inflight, -1)
			return &types.ImageResult{
				Images: [][]byte{pngBytes},
				Usage:  types.ImageUsage{ImageCount: 1, InputTokens: 2, OutputTokens: 3, TotalTokens: 5},
				ProviderMetadata: map[string]interface{}{
					"mock": map[string]interface{}{"images": []interface{}{map[string]interface{}{"revised": "x"}}},
				},
			}, nil
		},
	}
	res, err := GenerateImage(context.Background(), GenerateImageOptions{Model: model, Prompt: "p", N: intPtrT(3)})
	if err != nil {
		t.Fatal(err)
	}
	if peak < 2 {
		t.Fatalf("calls were sequential (peak concurrency %d)", peak)
	}
	if len(res.Images) != 3 || res.Usage.InputTokens != 6 || res.Usage.OutputTokens != 9 || res.Usage.TotalTokens != 15 {
		t.Fatalf("images=%d usage=%+v", len(res.Images), res.Usage)
	}
	if got := res.Images[2].ProviderMetadata["mock"].(map[string]interface{})["revised"]; got != "x" {
		t.Fatalf("per-image provider metadata = %v", res.Images[2].ProviderMetadata)
	}
}

func intPtrT(n int) *int { return &n }

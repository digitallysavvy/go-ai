package ai

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS embedMany awaits Promise.all per parallel group: it rejects as soon as
// one batch fails, while the siblings run on. Go waited for every sibling
// before returning the failure.
func TestR87ecarryEmbedManyFailsWithoutWaitingForSiblings(t *testing.T) {
	release := make(chan struct{})
	siblingDone := make(chan struct{})
	fastErr := errors.New("batch 1 failed")
	model := &testutil.MockEmbeddingModel{
		MaxEmbeddings:   1,
		ParallelSupport: true,
		DoEmbedManyFunc: func(ctx context.Context, inputs []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
			if inputs[0] == "a" {
				defer close(siblingDone)
				<-release
				return &types.EmbeddingsResult{Embeddings: [][]float64{{1}}}, nil
			}
			return nil, fastErr
		},
	}
	zero := 0
	type outcome struct{ err error }
	got := make(chan outcome, 1)
	go func() {
		_, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: []string{"a", "b"}, MaxRetries: &zero})
		got <- outcome{err}
	}()
	select {
	case o := <-got:
		if !errors.Is(o.err, fastErr) {
			t.Errorf("err = %v, want %v", o.err, fastErr)
		}
	case <-time.After(5 * time.Second):
		t.Error("EmbedMany waited for the sibling batch before returning the failure")
	}
	close(release)
	<-siblingDone
}

// A NaN token count is dropped from the root span (TS
// sanitizeAttributeValue) even when the Go-only counters hold a sum.
func TestR87ecarryEmbeddingTelemetryDropsNaNTokens(t *testing.T) {
	if u := telemetryUsageFromEmbeddingUsage(types.EmbeddingUsage{Tokens: math.NaN(), InputTokens: 5, TotalTokens: 5, Reported: true}, false); u.TotalTokens != nil {
		t.Fatalf("NaN tokens with counters: %+v, want no token count", u)
	}
}

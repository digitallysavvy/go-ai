package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 72 (agent area) tests for pkg/ai embed/rerank.

// TS embedMany awaits Promise.all per parallel group, which rejects with the
// first failure in time. A later batch that fails first wins over an earlier
// batch that fails later.
func TestR72agentEmbedManyParallelFirstFailureInTime(t *testing.T) {
	slowErr := errors.New("batch 0 failed late")
	fastErr := errors.New("batch 1 failed early")
	model := &testutil.MockEmbeddingModel{
		MaxEmbeddings:   1,
		ParallelSupport: true,
		DoEmbedManyFunc: func(ctx context.Context, inputs []string, opts *provider.EmbedModelOptions) (*types.EmbeddingsResult, error) {
			if inputs[0] == "a" {
				time.Sleep(100 * time.Millisecond)
				return nil, slowErr
			}
			return nil, fastErr
		},
	}
	zero := 0
	_, err := EmbedMany(context.Background(), EmbedManyOptions{Model: model, Inputs: []string{"a", "b"}, MaxRetries: &zero})
	if !errors.Is(err, fastErr) {
		t.Fatalf("err = %v, want the first failure in time (%v)", err, fastErr)
	}
}

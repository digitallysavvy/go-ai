package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS startBatch calls throwIfAborted after every request is normalized, the
// last one included, so a call aborted while the last request is prepared
// fails with the abort and never reaches doStartBatch.
func TestStartBatchAbortWhileNormalizingLastRequestSkipsStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := textBatchRequest("req-1", "m")
	req.Text.Tools = []types.Tool{{
		Name:       "s",
		Parameters: map[string]interface{}{"type": "object"},
		DescriptionFunc: func(context.Context, types.ToolDescriptionOptions) string {
			cancel()
			return "search"
		},
	}}
	started := false
	mock := &mockBatchV4{
		providerName: "mock.batch",
		startFn: func(context.Context, provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
			started = true
			return &provider.BatchV4StartResult{BatchID: "b"}, nil
		},
	}
	_, err := ExperimentalStartBatch(ctx, StartBatchOptions{Provider: mock, Requests: []BatchRequest{req}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if started {
		t.Fatal("DoStartBatch was called after the abort")
	}
}

package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
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

// TS builds these messages with template literals, so an ID or tool name with
// a quote or a non-ASCII character appears verbatim (Go %q would escape it).
func TestStartBatchErrorMessagesQuoteVerbatim(t *testing.T) {
	id := `ré"q`
	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: &mockBatchV4{providerName: "mock.batch"},
		Requests: []BatchRequest{textBatchRequest(id, "m"), textBatchRequest(id, "m")},
	})
	var invalid *providererrors.InvalidArgumentError
	if !errors.As(err, &invalid) || invalid.Message != `request IDs must be unique; duplicate ID "ré"q"` {
		t.Fatalf("err = %v", err)
	}

	obj := map[string]interface{}{"type": "object"}
	name := `sé"arch`
	a := []types.Tool{{Name: name, Parameters: obj, Description: "a"}}
	b := []types.Tool{{Name: name, Parameters: obj, Description: "b"}}
	err = startBatchWithTools(t, a, b, nil, nil)
	if !errors.As(err, &invalid) || invalid.Message != `tool "sé"arch" must have the same definition in every batch request` {
		t.Fatalf("err = %v", err)
	}
}

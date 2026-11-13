package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

var errR95provcAbortReason = errors.New("r95provc abort reason")

// TS executeStartStatusFlow races doStatus against the poll timeout: when the
// caller aborts during the status call, the race settles with doStatus's
// rejection, which is the abort reason (fetch rejects with signal.reason).
func TestR95provcGenerateVideoStatusAbortReturnsCause(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	release := make(chan struct{})
	defer close(release)
	blocking := func() (*provider.VideoModelV3OperationStatusResult, error) {
		cancel(errR95provcAbortReason)
		<-release // a DoStatus that ignores its context
		return r89provcPending()
	}
	model := &r89provcStartStatusOnlyModel{intervalMs: 1, statuses: []func() (*provider.VideoModelV3OperationStatusResult, error){blocking}}
	zero := 0
	_, err := GenerateVideo(ctx, GenerateVideoOptions{Model: model, Prompt: VideoPrompt{Text: "a cat"}, MaxRetries: &zero})
	if !errors.Is(err, errR95provcAbortReason) {
		t.Fatalf("err = %v, want the abort reason", err)
	}
}

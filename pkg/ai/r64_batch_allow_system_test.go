package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS TextBatchRequest takes Prompt's allowSystemInMessages: with it a system
// message in messages reaches the provider; without it standardizePrompt
// rejects the request.
func TestR64BatchTextRequestAllowSystemInMessages(t *testing.T) {
	var got []types.Message
	api := &mockBatchV4{providerName: "mock.batch", startFn: func(_ context.Context, o provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
		got = o.Requests[0].Text.Options.Prompt.Messages
		return &provider.BatchV4StartResult{BatchID: "b1"}, nil
	}}
	messages := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "be brief"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
	}
	req := func(allow bool) StartBatchOptions {
		return StartBatchOptions{Provider: api, Requests: []BatchRequest{{Text: &BatchTextRequest{ID: "r1", Model: "m", Messages: messages, AllowSystemInMessages: allow}}}}
	}
	if _, err := ExperimentalStartBatch(context.Background(), req(false)); err == nil {
		t.Fatal("system message without AllowSystemInMessages: want an error")
	}
	if _, err := ExperimentalStartBatch(context.Background(), req(true)); err != nil {
		t.Fatalf("AllowSystemInMessages: %v", err)
	}
	if len(got) != 2 || got[0].Role != types.RoleSystem {
		t.Fatalf("provider messages = %+v, want the system message first", got)
	}
}

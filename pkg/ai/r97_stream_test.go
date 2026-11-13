package ai

import (
	"context"
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ruling 4: a tool call's provider metadata holding a provider's 1e400 (read
// as +Inf) stays on the step's tool-call content, with the non-finite value
// written as null (JSON.stringify), instead of the whole metadata being
// dropped because json.Marshal fails on it. TS keeps the call's
// providerMetadata on the content part (and so on the next request's
// providerOptions).
func TestR97streamToolCallProviderMetadataKeepsNonFinite(t *testing.T) {
	call := types.ToolCall{
		ID:           "c1",
		ToolName:     "noop",
		Arguments:    map[string]interface{}{},
		RawArguments: `{}`,
		ProviderMetadata: map[string]interface{}{
			"p": map[string]interface{}{"sig": "keep", "x": math.Inf(1)},
		},
	}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeToolCall, ToolCall: &call},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool_calls", Usage: r71streamUsage},
		}),
		Prompt: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	var got string
	for _, part := range r.FinalStep().Content {
		if tc, ok := part.(types.ToolCallContent); ok {
			got = string(tc.ProviderMetadata)
		}
	}
	if want := `{"p":{"sig":"keep","x":null}}`; got != want {
		t.Fatalf("tool-call content providerMetadata = %q, want %q", got, want)
	}
}

// r97streamHiddenScore has a non-finite float that encoding/json never
// encodes (json:"-").
type r97streamHiddenScore struct {
	Label string  `json:"label"`
	Score float64 `json:"-"`
}

// SafeValidateUIMessages reports a non-finite number only where the
// messages' JSON holds one (known difference "Non-finite numbers in UI
// messages"): a field tagged json:"-" is not encoded, so a NaN there is not
// an invalid JSON value, and a NaN in an encoded field still is.
func TestR97streamValidateUIMessagesIgnoresUnencodedNonFinite(t *testing.T) {
	msgs := func(metadata interface{}) []UIMessage {
		return []UIMessage{{ID: "m1", Role: UIMessageRoleUser, Metadata: metadata, Parts: []UIMessagePart{TextUIPart{Text: "hi"}}}}
	}
	res := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: msgs(r97streamHiddenScore{Label: "a", Score: math.NaN()})})
	if !res.Success {
		t.Fatalf("hidden NaN: validation failed: %v", res.Error)
	}
	if got, _ := res.Data[0].Metadata.(map[string]interface{}); got["label"] != "a" || len(got) != 1 {
		t.Fatalf("metadata = %#v, want {label: a}", res.Data[0].Metadata)
	}
	res = SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: msgs(map[string]interface{}{"score": math.NaN()})})
	if res.Success || res.Error == nil || res.Error.Error() == "" {
		t.Fatalf("encoded NaN: want a validation error, got %#v", res)
	}
}

package bedrock

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func r55FinishMetadata(t *testing.T, chunks []*provider.StreamChunk) map[string]interface{} {
	t.Helper()
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeFinish {
			if c.ProviderMetadata == nil {
				return nil
			}
			var m map[string]interface{}
			if err := json.Unmarshal(c.ProviderMetadata, &m); err != nil {
				t.Fatal(err)
			}
			return m["amazonBedrock"].(map[string]interface{})
		}
	}
	t.Fatal("no finish chunk")
	return nil
}

// TS builds providerMetadata from each metadata event on its own and replaces
// the previous one; nullish cache fields and falsy traces are left out.
func TestR55BedrockStreamMetadataEventReplacesEarlierMetadata(t *testing.T) {
	chunks, err := r55CollectBedrockStream(t, [][2]string{
		{"metadata", `{"usage":{"inputTokens":1,"outputTokens":2,"cacheWriteInputTokens":3},"trace":{"a":1}}`},
		{"metadata", `{"usage":{"inputTokens":1,"outputTokens":2,"cacheWriteInputTokens":null},"serviceTier":{"type":"flex"},"trace":0}`},
		{"messageStop", `{"stopReason":"end_turn"}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := r55FinishMetadata(t, chunks)
	want := map[string]interface{}{"serviceTier": map[string]interface{}{"type": "flex"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata = %#v, want %#v", got, want)
	}
}

// TS reassigns stopSequence on every messageStop, so a later messageStop
// without a stop sequence clears it.
func TestR55BedrockStreamLaterMessageStopClearsStopSequence(t *testing.T) {
	chunks, err := r55CollectBedrockStream(t, [][2]string{
		{"messageStop", `{"stopReason":"stop_sequence","additionalModelResponseFields":{"delta":{"stop_sequence":"END"}}}`},
		{"messageStop", `{"stopReason":"end_turn"}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := r55FinishMetadata(t, chunks); got != nil {
		t.Fatalf("metadata = %#v, want none", got)
	}
}

// Generate: TS spreads cacheWriteInputTokens/cacheDetails only when != null
// and keeps trace only when it is an object.
func TestR55BedrockGenerateMetadataSkipsNullishCacheFieldsAndScalarTrace(t *testing.T) {
	m := newBedrockModelWithID("amazon.nova-pro-v1:0")
	r, err := m.convertConverseResponse([]byte(`{"output":{"message":{"role":"assistant","content":[{"text":"hi"}]}},"stopReason":"end_turn","trace":"x","usage":{"inputTokens":1,"outputTokens":2,"totalTokens":3,"cacheWriteInputTokens":null,"cacheDetails":null}}`), false, false)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"stopSequence": nil}
	if got := r.ProviderMetadata["amazonBedrock"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata = %#v, want %#v", got, want)
	}
}

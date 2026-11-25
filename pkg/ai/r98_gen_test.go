package ai

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

const r98genProtoMetadata = `{"p":{"__proto__":{"x":1},"constructor":{"prototype":{"y":2}},"sig":"keep"}}`

// TS stream-object.ts keeps the finish part's providerMetadata object as is
// (no re-parse), so __proto__ / constructor.prototype keys reach the result.
func TestR98genStreamObjectFinishMetadataKeepsProtoKeys(t *testing.T) {
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, Text: `{"a":1}`},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, ProviderMetadata: json.RawMessage(r98genProtoMetadata)},
		}), nil
	}}
	res, err := StreamObject(context.Background(), StreamObjectOptions{
		Model: model, Prompt: "hi",
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]interface{}
	if err := json.Unmarshal([]byte(r98genProtoMetadata), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.ProviderMetadata, want) {
		t.Fatalf("provider metadata = %#v, want %#v", res.ProviderMetadata, want)
	}
}

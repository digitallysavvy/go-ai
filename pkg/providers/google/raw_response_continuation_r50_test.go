package google

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS returns the last request's rawValue as response.body; the summed usage
// belongs only to usage and providerMetadata.usageMetadata.
func TestR50ContinuedRawResponseKeepsLastRequestUsage(t *testing.T) {
	srv, _ := scriptedServer(t,
		scriptedReply{body: `{"candidates":[{"content":{"parts":[{"text":"a"}]},"finishReason":"CONTINUATION","continuationToken":"t"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":4}}`},
		scriptedReply{body: `{"candidates":[{"content":{"parts":[{"text":"b"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":6}}`},
	)
	res, err := longDecodeModel(srv).DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(res.RawResponse)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		UsageMetadata struct {
			PromptTokenCount int `json:"promptTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.UsageMetadata.PromptTokenCount != 20 {
		t.Fatalf("raw body promptTokenCount = %d, want the last request's 20 (%s)", body.UsageMetadata.PromptTokenCount, raw)
	}
}

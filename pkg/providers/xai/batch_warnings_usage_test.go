package xai

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS XaiBatch#prepareRequest returns the full prepareRequest warning list,
// including the unsupported frequencyPenalty/stopSequences settings.
func TestBatchTextRequestKeepsUnsupportedSettingWarnings(t *testing.T) {
	b := NewBatch(New(Config{APIKey: "k"}))
	freq := 0.5
	_, _, warnings, err := b.prepareBatchRequest(provider.BatchV4Request{
		Type: provider.BatchRequestTypeText,
		Text: &provider.TextBatchV4Request{
			ID: "r1", ModelID: "grok-4",
			Options: provider.GenerateOptions{
				Prompt:           types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
				FrequencyPenalty: &freq,
				StopSequences:    []string{"x"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 2 || warnings[0].Feature != "frequencyPenalty" || warnings[1].Feature != "stopSequences" {
		t.Fatalf("warnings = %+v", warnings)
	}
}

// TS convertXaiBatchTextUsage keeps the parsed usage object as `raw`.
func TestBatchTextUsageKeepsRaw(t *testing.T) {
	var resp xaiBatchTextResponseWire
	body := `{"choices":[{"message":{"role":"assistant","content":"hi"},"index":0,"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3,"num_sources_used":4}}`
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	result, berr := convertXAIBatchTextResponse(resp)
	if berr != nil {
		t.Fatal(berr)
	}
	if result.Usage.Raw["num_sources_used"] != float64(4) {
		t.Fatalf("raw = %+v", result.Usage.Raw)
	}
}

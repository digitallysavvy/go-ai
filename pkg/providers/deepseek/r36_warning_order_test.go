package deepseek

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getArgs returns [...allWarnings, ...toolWarnings] with allWarnings in
// the order topK, seed, penalties, thinking.type, temperature, topP, then the
// reasoning-effort mapping.
func TestDeepSeekWarningOrderMatchesTS(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "deepseek-reasoner")
	high := types.ReasoningXHigh
	temp := 0.5
	freq := 0.1
	_, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:           types.Prompt{Text: "hi"},
		Temperature:      &temp,
		FrequencyPenalty: &freq,
		Reasoning:        &high,
		Tools:            []types.Tool{{Type: "provider", ProviderID: "x.y", Name: "y"}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, w := range warnings {
		got = append(got, w.Type+":"+w.Feature+w.Setting)
	}
	want := []string{
		"deprecated:frequencyPenalty",
		"unsupported:temperature",
		"compatibility:reasoning",
		"unsupported:provider-defined tool x.y",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %v, want %v", got, want)
	}
}

// TS providerMetadata keeps toolCallTypes for any non-null tool_calls (even
// empty) and logprobs for any non-null logprobs object.
func TestDeepSeekResponseMetadataKeepsEmptyToolCallTypesAndLogprobs(t *testing.T) {
	var resp deepseekResponse
	body := `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"x","tool_calls":[]},"logprobs":{"content":[]}}]}`
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	m := NewLanguageModel(New(Config{APIKey: "k"}), "deepseek-chat")
	res, err := m.convertResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	meta := res.ProviderMetadata["deepseek"].(map[string]interface{})
	if v, ok := meta["toolCallTypes"].([]string); !ok || len(v) != 0 {
		t.Errorf("toolCallTypes = %#v, want empty list", meta["toolCallTypes"])
	}
	lp, ok := meta["logprobs"].(map[string]interface{})
	if !ok {
		t.Fatalf("logprobs missing: %#v", meta)
	}
	if c, ok := lp["content"]; !ok || len(c.([]deepseekLogprobEntry)) != 0 {
		t.Errorf("logprobs.content = %#v, want empty list", lp["content"])
	}
}

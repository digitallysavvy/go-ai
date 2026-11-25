package quiverai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// OVC-01 probe: providerOptions.quiverai as the exported options struct or a
// map[string]string is validated like the map form (an unsupported effort
// fails instead of being dropped), and typed part options supply the
// reasoning metadata.
func TestR91toolsQuiverAITypedOptionLiterals(t *testing.T) {
	for name, options := range map[string]interface{}{
		"struct":            QuiverAILanguageModelOptions{ReasoningEffort: "bogus"},
		"map[string]string": map[string]string{"reasoningEffort": "bogus"},
	} {
		_, _, err := prepareQuiverAICall(&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"quiverai": options}})
		if err == nil {
			t.Errorf("%s: bogus reasoningEffort accepted", name)
		}
	}
	if _, _, err := prepareQuiverAICall(&provider.GenerateOptions{ProviderOptions: map[string]interface{}{"quiverai": map[string]string{"reasoningEffort": "high"}}}); err != nil {
		t.Errorf("valid typed options rejected: %v", err)
	}
	meta := quiverAIReasoningMetadata(types.ReasoningContent{ProviderOptions: map[string]interface{}{"quiverai": map[string]string{"itemId": "rs_1"}}})
	if meta["itemId"] != "rs_1" {
		t.Errorf("metadata = %v", meta)
	}
}

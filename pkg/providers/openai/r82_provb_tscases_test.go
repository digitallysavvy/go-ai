package openai

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

// TestR82provbTSCasesCompatConvertMessages replays
// convert-to-openai-compatible-chat-messages.test.ts (TS
// convertToOpenAICompatibleChatMessages, extracted at e64ed82f27 with
// --expand-each --fn=convertToOpenAICompatibleChatMessages) through the
// prompt converter with the options of the OpenAI-compatible chat model
// (Config.OpenAICompatible) and the TS defaults (provider
// "openaiCompatible", providerOptionsKey "google").
func TestR82provbTSCasesCompatConvertMessages(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/compat_convert_messages.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		msgs, why := tsfixture.BuildPromptMessages(args[0])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		opts := prompt.ToOpenAIMessagesOptions{
			Provider: "openaiCompatible", ThoughtSignatureKey: "google", AllowVideo: true,
			ReasoningKey: "reasoning_content", SpreadCompatMetadata: true, FileParts: prompt.FilePartsOpenAICompatible,
		}
		if len(args) > 1 {
			o, _ := args[1].(map[string]interface{})
			if s, ok := o["provider"].(string); ok {
				opts.Provider = s
			}
			if s, ok := o["providerOptionsKey"].(string); ok {
				opts.ThoughtSignatureKey = s
			}
			opts.SupportsMultiPartToolContent, _ = o["supportsMultiPartToolContent"].(bool)
		}
		out, err := prompt.ToOpenAIMessagesWithError(msgs, opts)
		if err != nil {
			return nil, nil, err
		}
		raw, _ := json.Marshal(out)
		var v interface{}
		_ = json.Unmarshal(raw, &v)
		if v == nil {
			v = []interface{}{}
		}
		return v, nil, nil
	}, map[string]string{})
}

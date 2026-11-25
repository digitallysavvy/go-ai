package openai

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

// TestR82provbTSCasesCompatPrepareTools replays
// openai-compatible-prepare-tools.test.ts (TS prepareTools, --expand-each
// --fn=prepareTools at e64ed82f27) through the OpenAI-compatible chat
// request builder (Config.OpenAICompatible).
func TestR82provbTSCasesCompatPrepareTools(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/compat_prepare_tools.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		tools, why := tsfixture.BuildTools(in["tools"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}, Tools: tools}
		if tc, ok := in["toolChoice"].(map[string]interface{}); ok && tc["__undef"] == nil {
			opts.ToolChoice = tsfixture.BuildToolChoice(tc)
		}
		m, err := New(Config{APIKey: "k", OpenAICompatible: true, Name: "test-provider"}).ChatModel("m")
		if err != nil {
			return nil, nil, err
		}
		body, warnings, err := m.(*LanguageModel).buildRequestBodyWithWarnings(opts, false)
		if err != nil {
			return nil, nil, err
		}
		out := map[string]interface{}{"toolWarnings": tsfixture.WarningsToTS(warnings)}
		for key, field := range map[string]string{"tools": "tools", "tool_choice": "toolChoice"} {
			if v, ok := body[key]; ok {
				b, _ := json.Marshal(v)
				var g interface{}
				_ = json.Unmarshal(b, &g)
				out[field] = g
			}
		}
		return out, nil, nil
	}, nil)
}

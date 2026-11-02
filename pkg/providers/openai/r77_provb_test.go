package openai

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR77ProvbTSCasesCapabilities replays openai-language-model-capabilities.test.ts
// (extract.mjs --expand-each --fn=getOpenAILanguageModelCapabilities: one case
// per it.each row) through GetLanguageModelCapabilities, rendered in the TS
// return shape.
func TestR77ProvbTSCasesCapabilities(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/capabilities.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		id, ok := args[0].(string)
		if !ok {
			return nil, nil, fmt.Errorf("%w: model id %v", tsfixture.ErrUnsupported, args[0])
		}
		c := GetLanguageModelCapabilities(id)
		out := map[string]interface{}{
			"isReasoningModel":               c.IsReasoningModel,
			"systemMessageMode":              c.SystemMessageMode,
			"supportsFlexProcessing":         c.SupportsFlexProcessing,
			"supportsPriorityProcessing":     c.SupportsPriorityProcessing,
			"supportsConfigurationUpdate":    c.SupportsConfigurationUpdate,
			"supportsAsyncToolCalling":       c.SupportsAsyncToolCalling,
			"supportsNonReasoningParameters": c.SupportsNonReasoningParameters,
		}
		if c.SupportedReasoningEfforts != nil {
			out["supportedReasoningEfforts"] = c.SupportedReasoningEfforts
		}
		return out, nil, nil
	}, nil)
}

// TestR77ProvbTSCasesChatUsage replays convert-openai-chat-usage.test.ts
// through convertOpenAIUsage (the TS wire usage object decoded as Go reads
// the response).
func TestR77ProvbTSCasesChatUsage(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/chat_usage.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		b, _ := json.Marshal(args[0])
		var u openAIUsage
		if err := json.Unmarshal(b, &u); err != nil {
			return nil, nil, err
		}
		return tsfixture.UsageToTS(convertOpenAIUsage(u)), nil, nil
	}, nil)
}

// TestR77ProvbTSCasesNormalizeJSONSchema replays
// normalize-openai-json-schema.test.ts through NormalizeOpenAIJSONSchema
// ({schema, warnings} in the TS shape; a rejected schema is an error).
func TestR77ProvbTSCasesNormalizeJSONSchema(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/normalize_json_schema.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		m, ok := args[0].(map[string]interface{})
		if !ok {
			return nil, nil, fmt.Errorf("%w: schema %v", tsfixture.ErrUnsupported, args[0])
		}
		schema, warnings, err := NormalizeOpenAIJSONSchema(m)
		if err != nil {
			return nil, nil, err
		}
		return map[string]interface{}{"schema": schema, "warnings": tsfixture.WarningsToTS(warnings)}, nil, nil
	}, nil)
}

// TestR77ProvbTSCasesChatPrepareTools replays openai-chat-prepare-tools.test.ts
// (TS prepareChatTools) through the chat request builder: tools,
// tool_choice and the tool warnings.
func TestR77ProvbTSCasesChatPrepareTools(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/chat_prepare_tools.json.gz")
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
		m, err := New(Config{APIKey: "k"}).ChatModel("gpt-4o")
		if err != nil {
			return nil, nil, err
		}
		lm, ok := m.(*LanguageModel)
		if !ok {
			return nil, nil, fmt.Errorf("%w: chat model is %T", tsfixture.ErrUnsupported, m)
		}
		body, warnings, err := lm.buildRequestBodyWithWarnings(opts, false)
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

// TestR77ProvbTSCasesChatConvertMessages replays
// convert-to-openai-chat-messages.test.ts (TS convertToOpenAIChatMessages)
// through the chat request builder: the request messages and warnings, with
// systemMessageMode passed as providerOptions.openai.systemMessageMode.
func TestR77ProvbTSCasesChatConvertMessages(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/chat_convert_messages.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		msgs, why := tsfixture.BuildPromptMessages(in["prompt"])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		opts := &provider.GenerateOptions{Prompt: types.Prompt{Messages: msgs}}
		if mode, ok := in["systemMessageMode"].(string); ok {
			opts.ProviderOptions = map[string]interface{}{"openai": map[string]interface{}{"systemMessageMode": mode}}
		}
		m, err := New(Config{APIKey: "k"}).ChatModel("gpt-4o")
		if err != nil {
			return nil, nil, err
		}
		body, warnings, err := m.(*LanguageModel).buildRequestBodyWithWarnings(opts, false)
		if err != nil {
			return nil, nil, err
		}
		b, _ := json.Marshal(body["messages"])
		var messages interface{}
		_ = json.Unmarshal(b, &messages)
		if messages == nil {
			messages = []interface{}{}
		}
		return map[string]interface{}{"messages": messages, "warnings": tsfixture.WarningsToTS(warnings)}, nil, nil
	}, nil)
}

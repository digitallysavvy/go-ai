package workflow

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS WorkflowAgent.prepareInvocation hands prepareCall the constructor's
// generation settings (`{...this.generationSettings}`) and merges the call's
// own settings over the hook's result afterwards, so the hook sees the agent
// values while the model call uses the call's.
func TestR73Carry_PrepareCallSeesAgentGenerationSettings(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	i := func(v int) *int { return &v }
	for _, stream := range []bool{false, true} {
		name := "generate"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			model := &wfJSONModel{}
			var seen LanguageModelCallOptions
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: model, Temperature: f(0.1), MaxTokens: i(10), MaxRetries: i(1),
				Headers:         map[string]string{"x-agent": "a"},
				ProviderOptions: map[string]interface{}{"p": map[string]interface{}{"agent": true}},
				PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					seen = o
					return o, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			callHeaders := map[string]string{"x-call": "c"}
			callProvider := map[string]interface{}{"p": map[string]interface{}{"call": true}}
			if stream {
				res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{
					Prompt: "hi", Temperature: f(0.9), MaxTokens: i(99), MaxRetries: i(0), Headers: callHeaders, ProviderOptions: callProvider,
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := res.ConsumeStream(); err != nil {
					t.Fatal(err)
				}
			} else if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{
				Prompt: "hi", Temperature: f(0.9), MaxTokens: i(99), MaxRetries: i(0), Headers: callHeaders, ProviderOptions: callProvider,
			}); err != nil {
				t.Fatal(err)
			}
			if seen.Temperature == nil || *seen.Temperature != 0.1 || seen.MaxTokens == nil || *seen.MaxTokens != 10 {
				t.Errorf("hook settings = temperature %v maxTokens %v, want agent 0.1/10", seen.Temperature, seen.MaxTokens)
			}
			if seen.MaxRetries == nil || *seen.MaxRetries != 1 {
				t.Errorf("hook maxRetries = %v, want agent 1", seen.MaxRetries)
			}
			if seen.Headers["x-agent"] != "a" || seen.Headers["x-call"] != "" {
				t.Errorf("hook headers = %v, want the agent's", seen.Headers)
			}
			if _, ok := seen.ProviderOptions["p"].(map[string]interface{})["agent"]; !ok {
				t.Errorf("hook providerOptions = %v, want the agent's", seen.ProviderOptions)
			}
			all := append(append([]*provider.GenerateOptions{}, model.opts...), model.wfMockModel.opts...)
			if len(all) == 0 {
				t.Fatal("no model call")
			}
			got := all[0]
			if got.Temperature == nil || *got.Temperature != 0.9 || got.MaxTokens == nil || *got.MaxTokens != 99 {
				t.Errorf("model settings = temperature %v maxTokens %v, want the call's 0.9/99", got.Temperature, got.MaxTokens)
			}
			if got.Headers["x-call"] != "c" || got.Headers["x-agent"] != "" {
				t.Errorf("model headers = %v, want the call's", got.Headers)
			}
		})
	}
}

// r73 prova CR: TS serializable-schema resolve makes a `type: 'provider'` def
// provider-executed only when `isProviderExecuted` is set, so a
// client-executed provider tool (anthropic bash, computer, text editor,
// memory) keeps running its Execute after a workflow round trip.
func TestR73Carry_ResolveClientProviderToolNotProviderExecuted(t *testing.T) {
	defs, err := SerializeToolSet([]types.Tool{
		{Name: "bash", Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.bash_20250124",
			ProviderArgs: map[string]interface{}{}, Parameters: map[string]interface{}{"type": "object"}, SupportsDeferredResults: true},
		{Name: "web", Type: types.ToolTypeProviderDefined, ProviderID: "anthropic.web_search_20250305",
			ProviderArgs: map[string]interface{}{}, Parameters: map[string]interface{}{"type": "object"}, ProviderExecuted: true, SupportsDeferredResults: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]types.Tool{}
	for _, tool := range ResolveSerializableTools(defs) {
		got[tool.Name] = tool
	}
	bash := got["bash"]
	if bash.ProviderExecuted || bash.SupportsDeferredResults {
		t.Errorf("bash: providerExecuted=%v supportsDeferredResults=%v, want false/false", bash.ProviderExecuted, bash.SupportsDeferredResults)
	}
	if bash.Type != types.ToolTypeProviderDefined || bash.ProviderID != "anthropic.bash_20250124" {
		t.Errorf("bash identity = %q %q", bash.Type, bash.ProviderID)
	}
	web := got["web"]
	if !web.ProviderExecuted || !web.SupportsDeferredResults || web.ProviderID != "anthropic.web_search_20250305" {
		t.Errorf("web: providerExecuted=%v supportsDeferredResults=%v id=%q", web.ProviderExecuted, web.SupportsDeferredResults, web.ProviderID)
	}
}

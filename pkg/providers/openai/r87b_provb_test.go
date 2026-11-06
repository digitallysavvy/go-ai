package openai

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// V061 45 sweep: the OpenAI-compatible chat mode spreads raw provider
// options like TS openai-compatible-chat-language-model.ts getArgs; a raw
// stream / stream_options is posted as is by doGenerate and replaced by
// doStream's own values (stream: true, stream_options include_usage or
// undefined). Expected bodies recorded with the TS chat model (bun).
func TestR87bprovbCompatRawStreamOptions(t *testing.T) {
	po := map[string]interface{}{"myHost": map[string]interface{}{"stream": "raw", "stream_options": map[string]interface{}{"x": 1}}}
	for _, tc := range []struct {
		name         string
		stream       bool
		includeUsage bool
		want         string
	}{
		{"generate", false, true, `{"stream":"raw","stream_options":{"x":1}}`},
		{"stream usage", true, true, `{"stream":true,"stream_options":{"include_usage":true}}`},
		{"stream no usage", true, false, `{"stream":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:0", OpenAICompatible: true, IncludeUsage: tc.includeUsage, Name: "my-host", ChatProviderName: "my-host.chat"})
			lm, _ := p.ChatModel("m")
			body, _, err := lm.(*LanguageModel).buildRequestBodyWithWarnings(&provider.GenerateOptions{Prompt: chatTestPrompt(), ProviderOptions: po}, tc.stream)
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]interface{}{}
			for _, k := range []string{"stream", "stream_options"} {
				if v, ok := body[k]; ok {
					got[k] = v
				}
			}
			if b, _ := json.Marshal(got); string(b) != tc.want {
				t.Fatalf("body stream fields = %s, want %s", b, tc.want)
			}
		})
	}
}

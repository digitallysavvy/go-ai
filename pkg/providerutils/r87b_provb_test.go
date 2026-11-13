package providerutils

import (
	"encoding/json"
	"testing"
)

// V061 45: TS 3fab50aa9b openai-compatible-chat-language-model.ts spreads
// raw provider options into getArgs; doGenerate posts args as is, doStream
// writes stream: true and stream_options (include_usage or undefined) after
// the spread. Recorded with the TS chat model (bun, providerOptions.togetherai
// {stream: "raw", stream_options: {x: 1}}): generate sends both raw values,
// stream sends stream: true and the model's own stream_options.
func TestR87bprovbRawStreamOptionsInGenerateOnly(t *testing.T) {
	po := map[string]interface{}{"togetherai": map[string]interface{}{"stream": "raw", "stream_options": map[string]interface{}{"x": 1}}}

	gen := map[string]interface{}{"model": "m"}
	ApplyOpenAICompatibleCallSettings(gen, nil, nil, nil, nil, "togetherai", po)
	if b, _ := json.Marshal(gen); string(b) != `{"model":"m","stream":"raw","stream_options":{"x":1}}` {
		t.Fatalf("generate body = %s", b)
	}

	stream := map[string]interface{}{"model": "m", "stream": true, "stream_options": map[string]interface{}{"include_usage": true}}
	ApplyOpenAICompatibleCallSettings(stream, nil, nil, nil, nil, "togetherai", po)
	if b, _ := json.Marshal(stream); string(b) != `{"model":"m","stream":true,"stream_options":{"include_usage":true}}` {
		t.Fatalf("stream body = %s", b)
	}
}

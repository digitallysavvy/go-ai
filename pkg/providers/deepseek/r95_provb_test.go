package deepseek

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TestR95provbEmptyStopSequencesSent: TS sends `stop: stopSequences`, so an
// empty array is sent as `stop: []`; only an absent value is left out.
func TestR95provbEmptyStopSequencesSent(t *testing.T) {
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, StopSequences: []string{}}
	body, _, err := NewLanguageModel(New(Config{APIKey: "k"}), "deepseek-chat").buildRequestBodyWithWarnings(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if stop, ok := body["stop"].([]string); !ok || len(stop) != 0 {
		t.Fatalf("stop = %#v, want []", body["stop"])
	}
}

// TestR95provbResponsesGenerateValidatesBody: TS doGenerate parses the body
// with createJsonResponseHandler(deepseekResponsesResponseSchema), so a usage
// count that is not a number or an output item that is not an object with a
// string type is an "Invalid JSON response" error, not a result with zero
// usage or a skipped item.
func TestR95provbResponsesGenerateValidatesBody(t *testing.T) {
	for name, body := range map[string]string{
		"usage count":     `{"id":"r1","status":"completed","output":[],"usage":{"input_tokens":"3"}}`,
		"nested usage":    `{"id":"r1","status":"completed","output":[],"usage":{"server_tool_use":{"tool_search":"1"}}}`,
		"non-object item": `{"id":"r1","status":"completed","output":[5]}`,
		"item type":       `{"id":"r1","status":"completed","output":[{"type":1}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			lm := r94provbDeepSeekServer(t, body, "")
			_, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
			if !providerutils.IsInvalidJSONResponse(err) {
				t.Fatalf("err = %v, want an Invalid JSON response error", err)
			}
		})
	}
	// A valid body with nulls and an unknown item still succeeds.
	lm := r94provbDeepSeekServer(t, `{"id":null,"status":"completed","output":[{"type":"future","x":1}],"usage":{"input_tokens":null,"server_tool_use":null}}`, "")
	if _, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatalf("valid body: %v", err)
	}
}

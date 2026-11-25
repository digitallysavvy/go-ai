package openai

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS getArgs warning order: reasoningEffort, topK, reasoningSummary, message
// warnings, response format, promptCacheRetention, reasoning-model removals,
// serviceTier, then the tool warnings. topK is unsupported on chat.
func TestChatWarningOrderMatchesTS(t *testing.T) {
	m := NewLanguageModel(New(Config{APIKey: "k"}), "o1")
	topK := 3
	temp := 0.5
	_, warnings, err := m.buildRequestBodyWithWarnings(&provider.GenerateOptions{
		Prompt:      types.Prompt{Text: "hi"},
		TopK:        &topK,
		Temperature: &temp,
		Tools:       []types.Tool{{Type: "provider", ProviderID: "x.y", Name: "y"}},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{
			"reasoningSummary": "auto", "serviceTier": "flex",
		}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, w := range warnings {
		got = append(got, w.Type+":"+w.Feature)
	}
	want := []string{
		"unsupported:topK",
		"unsupported:reasoningSummary",
		"unsupported:temperature",
		"unsupported:serviceTier",
		"unsupported:tool type: provider",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("warnings = %v, want %v", got, want)
	}
}

// Only native OpenAI models fail the stream on an error before any output
// (TS throwIfOpenAIStreamErrorBeforeOutput); the openai-compatible chat model
// emits an error part and finishes with reason error.
func TestCompatStreamErrorBeforeOutputIsAnErrorPart(t *testing.T) {
	sse := `data: {"error":{"message":"boom","type":"server_error"}}` + "\n\n" + "data: [DONE]\n\n"
	s := newOpenAIStreamWithMetadata(io.NopCloser(strings.NewReader(sse)), false, "baseten.chat", nil)
	s.compat = true
	var sawError bool
	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			if err != io.EOF {
				t.Fatalf("Next returned error %v, want an error part", err)
			}
			break
		}
		switch c.Type {
		case provider.ChunkTypeError:
			sawError = true
		case provider.ChunkTypeFinish:
			finish = c
		}
	}
	if !sawError || finish == nil || finish.FinishReason != types.FinishReasonError {
		t.Fatalf("sawError=%v finish=%+v, want an error part and an error finish", sawError, finish)
	}
}

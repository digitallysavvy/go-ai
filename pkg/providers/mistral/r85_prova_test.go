package mistral

import (
	"context"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// convert-to-mistral-chat-messages.ts serializeMistralToolCallArguments (TS
// 4ae6986cd8): a string input that is valid JSON is sent as that text, text
// that is not JSON as a JSON string, other JSON as is.
func TestR85provaMistralToolCallArguments(t *testing.T) {
	for raw, want := range map[string]string{
		`"{\"b\":2,\"a\":1}"`: `{"b":2,"a":1}`,
		`{"a":1}`:             `{"a":1}`,
		`{"a":`:               `"{\"a\":"`,
		`"x"`:                 `"x"`,
	} {
		calls := mistralToolCalls([]types.ToolCall{{ID: "c", ToolName: "t", RawArguments: raw}})
		got := calls[0]["function"].(map[string]interface{})["arguments"]
		if got != want {
			t.Errorf("%s: arguments = %v, want %s", raw, got, want)
		}
	}
}

// CR r85 provb -> prova item 1 (TS response.text() before JSON.parse): a
// 2xx JSON body with a BOM parses.
func TestR85provaMistralTranscriptionBOMBody(t *testing.T) {
	p := newMistralProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		return mistralJSONResponse("\uFEFF" + `{"model":"voxtral-mini-latest","text":"hello","language":"en","segments":[],"usage":{"prompt_audio_seconds":1}}`), nil
	})
	res, err := NewTranscriptionModel(p, "voxtral-mini-latest").DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"})
	if err != nil || res.Text != "hello" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

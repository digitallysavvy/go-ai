package mistral

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"golang.org/x/net/websocket"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS reads part.providerOptions (the replayed providerMetadata object) as is:
// a number beyond the double range is Infinity there (ruling 4) and the
// mistral name still applies. encoding/json failed the whole decode, so the
// built-in call lost its wire name (cr-r99-gen-to-provg).
func TestR99provgConversationReplayMetadataOutOfRange(t *testing.T) {
	msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{
		ToolCallID: "c1", ToolName: "web_search", Input: `{"query":"q"}`, ProviderExecuted: true,
		ProviderMetadata: json.RawMessage(`{"mistral":{"name":"web_search_premium","score":1e400}}`),
	}}}}
	inputs, _, err := convertToMistralConversationInputs(msgs, newConversationToolNames(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) == 0 || inputs[0]["name"] != "web_search_premium" {
		t.Fatalf("inputs = %#v, want the metadata wire name", inputs)
	}
}

// TS returns usage: response.usage, the zod-parsed object: a nullish key the
// API sent as null stays null, an absent key stays absent.
func TestR99provgTranscriptionUsageKeepsNull(t *testing.T) {
	p := newMistralProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		return mistralJSONResponse(`{"model":"voxtral-mini-latest","text":"hi","usage":{"prompt_tokens":5,"completion_tokens":null}}`), nil
	})
	res, err := NewTranscriptionModel(p, "voxtral-mini-latest").DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	if err != nil {
		t.Fatal(err)
	}
	usage := res.ProviderUsage
	v, present := usage["completion_tokens"]
	_, totalPresent := usage["total_tokens"]
	if !present || v != nil || usage["prompt_tokens"] != 5 || totalPresent {
		t.Fatalf("usage = %#v, want completion_tokens null and total_tokens absent", usage)
	}
}

// TS safeParseJSON({text, schema: eventEnvelopeSchema}): a frame that is
// valid JSON but not an object with a string type fails the schema (a
// TypeValidationError), not the JSON parse. Go decoded into a map and called
// an array or a string frame a JSONParseError.
func TestR99provgRealtimeNonObjectFrameIsTypeValidation(t *testing.T) {
	for _, frame := range []string{`["x"]`, `"text"`, `5`} {
		rs := r87provaStartRealtime(t, false, func(send func(string), _ *websocket.Conn) { send(frame) })
		audio := &r87provaAudio{block: make(chan struct{})}
		res, err := r87provaRealtimeModel(t, rs, Config{}).DoStream(context.Background(), &provider.TranscriptionStreamOptions{
			Audio: audio, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = r87provaDrain(t, res.Stream)
		var parseErr *providererrors.JSONParseError
		if err == nil || errors.As(err, &parseErr) || !strings.HasPrefix(err.Error(), "Type validation failed") {
			t.Fatalf("%s: err = %#v, want the type validation error", frame, err)
		}
	}
}

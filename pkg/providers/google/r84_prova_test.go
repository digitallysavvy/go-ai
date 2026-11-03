package google

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS `new Error(message.error.message ?? 'Google Live API error')`: an empty
// server error message is kept, only an absent one takes the default.
func TestR84provaLiveEmptyErrorMessageKept(t *testing.T) {
	for _, msg := range []map[string]interface{}{{"message": ""}, {}} {
		want := "Google Live API error"
		if _, ok := msg["message"]; ok {
			want = ""
		}
		t.Run("transcription", func(t *testing.T) {
			server := newLiveTranscriptionTestServer(t)
			defer server.close()
			model := newLiveTestModel(t, server.ts.URL)
			result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
				Audio:            newChanAudioStream([]byte{1}),
				InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer result.Stream.Close() //nolint:errcheck
			server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
			server.toSend <- map[string]interface{}{"error": msg}
			_, err = drainUntilFinishOrError(t, result.Stream)
			if err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %q", err, want)
			}
		})
		t.Run("translation", func(t *testing.T) {
			server := newLiveTranscriptionTestServer(t)
			defer server.close()
			model := newSpeechTranslationTestModel(t, server.ts.URL, 0)
			result, err := model.DoStream(context.Background(), &provider.SpeechTranslationStreamOptions{
				Audio:            newChanAudioStream([]byte{1}),
				InputAudioFormat: provider.AudioFormat{Type: "audio/pcm"},
				TargetLanguage:   "es",
			})
			if err != nil {
				t.Fatal(err)
			}
			defer result.Stream.Close() //nolint:errcheck
			server.waitFor(t, func(m map[string]interface{}) bool { _, ok := m["setup"]; return ok }, time.Second)
			server.toSend <- map[string]interface{}{"error": msg}
			_, err = drainUntilTranslationFinishOrError(t, result.Stream)
			if err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %q", err, want)
			}
		})
	}
}

// TestR84provaInteractionsStepFieldsKept: TS convertToGoogleInteractionsInput
// sends a function_call step's id, name and arguments as given (a zero-arg
// call keeps `arguments: {}`), and a processing_call / processing_result step
// whenever processingId / processingCallId is a string, an empty one included.
func TestR84provaInteractionsStepFieldsKept(t *testing.T) {
	m := &InteractionsLanguageModel{}
	steps, _, warnings, err := m.convertPrompt(types.Prompt{Messages: []types.Message{{Role: types.RoleAssistant,
		Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "c", ToolName: "f", Input: `{}`},
			types.CustomContent{Kind: "google.processing_call", ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"processingId": ""}}},
			types.CustomContent{Kind: "google.processing_result", ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"processingCallId": ""}}},
		},
		ToolCalls: []types.ToolCall{{ID: "", ToolName: "g"}},
	}}}, GoogleInteractionsProviderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v", warnings)
	}
	got, _ := json.Marshal(steps)
	const want = `[{"arguments":{},"id":"c","name":"f","type":"function_call"},{"id":"","type":"processing_call"},{"call_id":"","type":"processing_result"},{"arguments":{},"id":"","name":"g","type":"function_call"}]`
	if string(got) != want {
		t.Errorf("steps =\n%s\nwant\n%s", got, want)
	}
}

// TestR84provaInteractionsEmptyOptionValuesKept: TS pruneUndefined drops
// only absent response format fields (an empty mime_type or schema object is
// sent), and `imageConfig != null` makes an empty imageConfig warn and add
// the default image entry.
func TestR84provaInteractionsEmptyOptionValuesKept(t *testing.T) {
	m := &InteractionsLanguageModel{modelID: "gemini-3-flash-preview"}
	body, warnings, _, err := m.buildArgs(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{
			"responseFormat": []interface{}{map[string]interface{}{"type": "text", "mimeType": "", "schema": map[string]interface{}{}}},
			"imageConfig":    map[string]interface{}{},
		}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(body.ResponseFormat)
	const want = `[{"mime_type":"","schema":{},"type":"text"},{"mime_type":"image/png","type":"image"}]`
	if string(got) != want {
		t.Errorf("response_format = %s, want %s", got, want)
	}
	if len(warnings) != 1 || warnings[0].Message != "google.interactions: providerOptions.google.imageConfig is deprecated. Use providerOptions.google.responseFormat with a { type: \"image\", ... } entry instead." {
		t.Errorf("warnings = %v", warnings)
	}
}

// TestR84provaSynthesizedToolCallEmptySignature: TS synthesizes the agent
// stream's tool-call with the parsed part's providerMetadata, which keeps an
// empty signature (signature != null), so the next turn sends it back.
func TestR84provaSynthesizedToolCallEmptySignature(t *testing.T) {
	empty := ""
	resp := interactionsResponse{ID: "int-1", Status: InteractionStatusCompleted}
	resp.Steps = []interactionsContentBlock{{Type: "function_call", ID: "c1", Name: "lookup", Arguments: []byte(`{}`), Signature: &empty}}
	var meta map[string]interface{}
	for _, c := range drainInteractions(t, newSynthesizedInteractionsStream(&InteractionsLanguageModel{}, resp, nil, nil, false)) {
		if c.Type == provider.ChunkTypeToolCall {
			meta = c.ToolCall.ProviderMetadata
		}
	}
	got, _ := json.Marshal(meta)
	if string(got) != `{"google":{"interactionId":"int-1","signature":""}}` {
		t.Errorf("tool call providerMetadata = %s", got)
	}
}

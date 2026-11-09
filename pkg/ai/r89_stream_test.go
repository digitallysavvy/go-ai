package ai

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r89streamEmptyCitationMessages is a UI message whose text part carries a
// url citation and a document citation with empty strings in every required
// string field. TS citationSchema (types/citation.ts) types them as
// z.string(), which accepts "": node with
// packages/ai/dist validateUIMessages returns the messages unchanged.
const r89streamEmptyCitationMessages = `[{"id":"1","role":"assistant","parts":[{"type":"text","text":"hi","citations":[` +
	`{"source":{"type":"source","sourceType":"url","id":"","url":""}},` +
	`{"source":{"type":"source","sourceType":"document","id":"","mediaType":"","title":""}}]}]}]`

// TS validateUIMessages accepts citations whose id, url, mediaType or title
// is "" (citationSchema uses z.string(), not z.string().min(1)).
func TestR89streamValidateUIMessagesAcceptsEmptyCitationStrings(t *testing.T) {
	messages, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: json.RawMessage(r89streamEmptyCitationMessages)})
	if err != nil {
		t.Fatalf("ValidateUIMessages: %v (TS accepts empty citation strings)", err)
	}
	text, ok := messages[0].Parts[0].(*TextUIPart)
	if !ok || len(text.Citations) != 2 {
		t.Fatalf("parts[0] = %#v, want a text part with 2 citations", messages[0].Parts[0])
	}
	if got := text.Citations[1].Source.SourceType; got != "document" {
		t.Errorf("citations[1].source.sourceType = %q, want document", got)
	}
}

// A UI message decodes like TS JSON.parse: empty citation strings are kept.
func TestR89streamUIMessageDecodesEmptyCitationStrings(t *testing.T) {
	var messages []UIMessage
	if err := json.Unmarshal([]byte(r89streamEmptyCitationMessages), &messages); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	snapshot, err := UIMessageFromChunk(UIMessageChunk{"id": "1", "role": "assistant", "parts": []interface{}{
		map[string]interface{}{"type": "text", "text": "hi", "citations": []interface{}{
			map[string]interface{}{"source": map[string]interface{}{"type": "source", "sourceType": "document", "id": "", "mediaType": "", "title": ""}},
		}},
	}})
	if err != nil {
		t.Fatalf("UIMessageFromChunk: %v", err)
	}
	if text, ok := snapshot.Parts[0].(*TextUIPart); !ok || len(text.Citations) != 1 {
		t.Fatalf("snapshot parts[0] = %#v, want a text part with 1 citation", snapshot.Parts[0])
	}
}

// r89streamTSSteerSmoothUI is the TS output (node, packages/ai/dist at
// 382d0b8eaf) of a two-step run: a tool call inside an open text block, the
// tool steers, the second step streams reasoning and text, with
// smoothStream({delayInMs: null}) and a messageMetadata callback that marks
// the steering-message and finish-step parts.
var r89streamTSSteerSmoothUI = []string{
	`{"type":"start"}`, `{"type":"start-step"}`, `{"type":"text-start","id":"t0"}`,
	`{"type":"text-delta","id":"t0","delta":"Let "}`, `{"type":"text-delta","id":"t0","delta":"me "}`,
	`{"type":"text-delta","id":"t0","delta":"check "}`, `{"type":"text-delta","id":"t0","delta":"the "}`,
	`{"type":"text-delta","id":"t0","delta":"weath"}`,
	`{"type":"tool-input-available","toolCallId":"call-1","toolName":"weather","input":{"city":"Paris"}}`,
	`{"type":"text-delta","id":"t0","delta":"er "}`, `{"type":"text-delta","id":"t0","delta":"now."}`,
	`{"type":"text-end","id":"t0"}`, `{"type":"tool-output-available","toolCallId":"call-1","output":"sunny"}`,
	`{"type":"message-metadata","messageMetadata":{"last":"finish-step"}}`, `{"type":"finish-step"}`,
	`{"type":"steering-message","id":"steer-1","parts":[{"type":"text","text":"I meant Paris, Texas."}]}`,
	`{"type":"message-metadata","messageMetadata":{"last":"steering-message"}}`,
	`{"type":"start-step"}`, `{"type":"reasoning-start","id":"r1"}`,
	`{"type":"reasoning-delta","id":"r1","delta":"Texas "}`, `{"type":"reasoning-delta","id":"r1","delta":"then"}`,
	`{"type":"reasoning-end","id":"r1"}`, `{"type":"text-start","id":"1"}`,
	`{"type":"text-delta","id":"1","delta":"It "}`, `{"type":"text-delta","id":"1","delta":"is "}`,
	`{"type":"text-delta","id":"1","delta":"sunny "}`, `{"type":"text-delta","id":"1","delta":"in "}`,
	`{"type":"text-delta","id":"1","delta":"Paris, "}`, `{"type":"text-delta","id":"1","delta":"Texas."}`,
	`{"type":"text-end","id":"1"}`,
	`{"type":"message-metadata","messageMetadata":{"last":"finish-step"}}`, `{"type":"finish-step"}`,
	`{"type":"finish","finishReason":"stop"}`,
}

// Differential: the same run in Go gives the TS UI chunks (SSE JSON) and the
// TS onChunk part order.
func TestR89streamSteeringWithSmoothStreamMatchesTS(t *testing.T) {
	steering := NewSteering()
	smooth, err := SmoothStream(SmoothStreamOptions{DelayInMs: new(int)})
	if err != nil {
		t.Fatal(err)
	}
	var onChunk []string
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r88streamModel(
			[]provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "t0"},
				{Type: provider.ChunkTypeText, ID: "t0", Text: "Let me check "},
				{Type: provider.ChunkTypeText, ID: "t0", Text: "the weath"},
				r88streamToolCallChunks[0],
				{Type: provider.ChunkTypeText, ID: "t0", Text: "er now."},
				{Type: provider.ChunkTypeTextEnd, ID: "t0"},
				r88streamToolCallChunks[1],
			},
			[]provider.StreamChunk{
				{Type: provider.ChunkTypeReasoningStart, ID: "r1"},
				{Type: provider.ChunkTypeReasoning, ID: "r1", Reasoning: "Texas then"},
				{Type: provider.ChunkTypeReasoningEnd, ID: "r1"},
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: "It is sunny in "},
				{Type: provider.ChunkTypeText, ID: "1", Text: "Paris, Texas."},
				{Type: provider.ChunkTypeTextEnd, ID: "1"},
				r88streamFinishStop,
			},
		),
		Prompt: "weather?",
		Tools: r88streamWeatherTool(func() {
			steering.Steer("I meant Paris, Texas.", WithSteerID("steer-1"))
		}),
		StopWhen:              []StopCondition{IsStepCount(5)},
		ExperimentalSteering:  steering,
		ExperimentalTransform: []StreamTransformFunc{smooth},
		OnChunk: func(c provider.StreamChunk) {
			// The Go-only lifecycle markers (v0.5) have no TS part.
			if c.Type != provider.ChunkTypeFirstChunk && c.Type != provider.ChunkTypeStreamFinish {
				onChunk = append(onChunk, string(c.Type))
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ch, errCh := r.ToUIMessageStream(context.Background(), UIMessageStreamResultOptions{
		MessageMetadata: func(part map[string]interface{}) map[string]interface{} {
			if typ := part["type"]; typ == "steering-message" || typ == "finish-step" {
				return map[string]interface{}{"last": typ}
			}
			return nil
		},
	})
	var got []string
	for c := range ch {
		b, err := marshalUIChunkInTSOrder(c)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(b))
	}
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(got, r89streamTSSteerSmoothUI) {
		t.Errorf("UI chunks:\n got %s\nwant %s", strings.Join(got, "\n     "), strings.Join(r89streamTSSteerSmoothUI, "\n     "))
	}
	wantOnChunk := []string{"start", "start-step", "text-start", "text", "text", "text", "text", "text", "tool-call", "text", "text", "text-end",
		"tool-result", "finish-step", "steering-message", "start-step", "reasoning-start", "reasoning", "reasoning", "reasoning-end",
		"text-start", "text", "text", "text", "text", "text", "text", "text-end", "finish-step", "finish"}
	if !reflect.DeepEqual(onChunk, wantOnChunk) {
		t.Errorf("onChunk:\n got %q\nwant %q", onChunk, wantOnChunk)
	}
}

// TS validateUIMessages (node, packages/ai/dist at 382d0b8eaf) strips state
// and citations from a steering message's text part, without validating the
// citations: the steering text schema is {type, text, providerMetadata?}
// (validate-ui-messages.ts 144-164).
func TestR89streamValidateUIMessagesStripsSteeringTextExtras(t *testing.T) {
	in := `[{"id":"1","role":"assistant","parts":[{"type":"steering-message","id":"s1","parts":[` +
		`{"type":"text","text":"hi","state":"done","citations":[{"source":{"type":"source","sourceType":"url","id":"a"}}],"providerMetadata":{"p":{"k":1}}},` +
		`{"type":"file","mediaType":"image/png","url":"https://x/y.png","extra":1}]}]}]`
	messages, err := ValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: json.RawMessage(in)})
	if err != nil {
		t.Fatalf("ValidateUIMessages: %v (TS accepts and strips)", err)
	}
	got, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"id":"1","role":"assistant","parts":[{"type":"steering-message","id":"s1","parts":[{"type":"text","text":"hi","providerMetadata":{"p":{"k":1}}},{"type":"file","mediaType":"image/png","url":"https://x/y.png"}]}]}]`
	if string(got) != want {
		t.Errorf("messages:\n got %s\nwant %s", got, want)
	}
}

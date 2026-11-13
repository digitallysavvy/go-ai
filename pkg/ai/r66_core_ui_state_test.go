package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS processUIMessageStream passes no title for tool-input-error: a part it
// creates has none, and an existing part keeps its own title.
func TestR66ToolInputErrorIgnoresChunkTitle(t *testing.T) {
	s := newUIMessageCallbackState(nil, "m")
	s.apply(UIMessageChunk{"type": "tool-input-error", "toolCallId": "c1", "toolName": "w", "input": "x", "errorText": "bad", "title": "Weather"}, nil)
	s.apply(UIMessageChunk{"type": "tool-input-start", "toolCallId": "c2", "toolName": "w", "title": "Own"}, nil)
	s.apply(UIMessageChunk{"type": "tool-input-error", "toolCallId": "c2", "toolName": "w", "input": "x", "errorText": "bad", "title": "Other"}, nil)
	parts := uiParts(s.message)
	if len(parts) != 2 {
		t.Fatalf("parts = %v", parts)
	}
	if title, ok := asUIPartChunk(parts[0])["title"]; ok {
		t.Errorf("new part title = %v, want none", title)
	}
	if title := asUIPartChunk(parts[1])["title"]; title != "Own" {
		t.Errorf("existing part title = %v, want Own", title)
	}
}

// TS updates a data part whose id matches, an empty id included
// (`dataChunk.id != null`).
func TestR66DataPartEmptyIDUpdatesInPlace(t *testing.T) {
	s := newUIMessageCallbackState(nil, "m")
	s.apply(UIMessageChunk{"type": "data-x", "id": "", "data": 1}, nil)
	s.apply(UIMessageChunk{"type": "data-x", "id": "", "data": 2}, nil)
	parts := uiParts(s.message)
	if len(parts) != 1 || asUIPartChunk(parts[0])["data"] != 2 {
		t.Fatalf("parts = %v, want one data part with data 2", parts)
	}
}

// TS applies a start chunk's messageId when it is not null, an empty id
// included.
func TestR66StartChunkEmptyMessageIDApplied(t *testing.T) {
	s := newUIMessageCallbackState(nil, "m")
	s.apply(UIMessageChunk{"type": "start", "messageId": ""}, nil)
	if id, ok := s.message["id"]; !ok || id != "" {
		t.Fatalf("message id = %v, want empty", id)
	}
}

// TS generateText/streamText start events carry the standardized prompt's
// messages: a prompt string is reported as one user message.
func TestR66StartEventMessagesFromPromptString(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
		},
	}
	check := func(label string, msgs []types.Message) {
		t.Helper()
		if len(msgs) != 1 || msgs[0].Role != types.RoleUser || len(msgs[0].Content) != 1 {
			t.Fatalf("%s start messages = %+v, want one user message", label, msgs)
		}
		if text, ok := msgs[0].Content[0].(types.TextContent); !ok || text.Text != "hi" {
			t.Fatalf("%s start message content = %+v", label, msgs[0].Content)
		}
	}
	var genMsgs []types.Message
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", OnStart: func(_ context.Context, e OnStartEvent) { genMsgs = e.Messages }}); err != nil {
		t.Fatal(err)
	}
	check("GenerateText", genMsgs)

	var streamMsgs []types.Message
	res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", OnStart: func(_ context.Context, e OnStartEvent) { streamMsgs = e.Messages }})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Text()
	check("StreamText", streamMsgs)
}

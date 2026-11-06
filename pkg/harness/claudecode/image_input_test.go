package claudecode_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

// promptStart runs one prompt turn and returns the `prompt` field of the
// start frame the bridge received.
func promptStart(t *testing.T, prompt harness.Prompt) (any, error) {
	t.Helper()
	got := make(chan map[string]any, 1)
	_, sess := startedHarness(t, claudecode.Settings{}, func(turn *bridgetest.Turn, start map[string]any) {
		got <- start
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
		})
	})
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: prompt, Emit: func(harness.StreamPart) {},
	})
	if err != nil {
		return nil, err
	}
	select {
	case start := <-got:
		<-control.Done()
		return start["prompt"], nil
	case <-time.After(2 * time.Second):
		t.Fatal("bridge never received a start frame")
		return nil, nil
	}
}

// TS (#23469): "sends a text-only user message as a string prompt"
func TestDoPromptTurn_TextOnlyMessageSendsStringPrompt(t *testing.T) {
	prompt, err := promptStart(t, harness.Prompt{Message: &types.Message{Role: types.RoleUser, Content: []types.ContentPart{
		types.TextContent{Text: "a"}, types.TextContent{Text: "b"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if prompt != "a\n\nb" {
		t.Fatalf("prompt = %#v, want joined string", prompt)
	}
}

// TS (#23469): "sends inline images as image blocks"
func TestDoPromptTurn_InlineImagesSentAsImageBlocks(t *testing.T) {
	prompt, err := promptStart(t, harness.Prompt{Message: &types.Message{Role: types.RoleUser, Content: []types.ContentPart{
		types.TextContent{Text: "What is in this image?"},
		types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "data:image/png;base64," + tinyPNG}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	want := []any{
		map[string]any{"type": "text", "text": "What is in this image?"},
		map[string]any{"type": "image", "data": tinyPNG, "mediaType": "image/png"},
	}
	if !reflect.DeepEqual(prompt, want) {
		t.Fatalf("prompt = %#v, want %#v", prompt, want)
	}
}

// TS (#23469): rejects remote URLs etc. before any start frame is sent.
func TestDoPromptTurn_RejectsUnsupportedImagePart(t *testing.T) {
	_, err := promptStart(t, harness.Prompt{Message: &types.Message{Role: types.RoleUser, Content: []types.ContentPart{
		types.FileContent{MediaType: "image/png", URL: "https://example.com/a.png"},
	}}})
	var unsupported *harness.CapabilityUnsupportedError
	if !asCapabilityUnsupported(err, &unsupported) {
		t.Fatalf("err = %v, want CapabilityUnsupportedError", err)
	}
	if !strings.Contains(err.Error(), "remote image URLs") {
		t.Fatalf("err = %v", err)
	}
}

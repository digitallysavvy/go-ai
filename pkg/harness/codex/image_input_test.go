package codex_test

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/codex"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func promptStart(t *testing.T, prompt harness.Prompt) (any, error) {
	t.Helper()
	got := make(chan map[string]any, 1)
	_, sess := startedHarness(t, codex.Settings{}, func(turn *bridgetest.Turn, start map[string]any) {
		got <- start
		turn.Emit(finishFrame())
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

// TS (#23577): normalized text and image parts reach the bridge in order.
func TestDoPromptTurn_InlineImagesSentAsPromptParts(t *testing.T) {
	prompt, err := promptStart(t, harness.Prompt{Message: &types.Message{Role: types.RoleUser, Content: []types.ContentPart{
		types.TextContent{Text: "Describe"},
		types.ImageContent{Image: mustB64(t, tinyPNG), MimeType: "image/png"},
		types.TextContent{Text: "briefly"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	want := []any{
		map[string]any{"type": "text", "text": "Describe"},
		map[string]any{"type": "image", "data": tinyPNG, "mediaType": "image/png"},
		map[string]any{"type": "text", "text": "briefly"},
	}
	if !reflect.DeepEqual(prompt, want) {
		t.Fatalf("prompt = %#v, want %#v", prompt, want)
	}
}

func TestDoPromptTurn_TextOnlyMessageSendsStringPrompt(t *testing.T) {
	prompt, err := promptStart(t, harness.Prompt{Message: &types.Message{Role: types.RoleUser, Content: []types.ContentPart{
		types.TextContent{Text: "a"}, types.TextContent{Text: "b"},
	}}})
	if err != nil || prompt != "a\n\nb" {
		t.Fatalf("prompt = %#v, err = %v", prompt, err)
	}
}

func TestDoPromptTurn_RejectsRemoteImageURL(t *testing.T) {
	_, err := promptStart(t, harness.Prompt{Message: &types.Message{Role: types.RoleUser, Content: []types.ContentPart{
		types.ImageContent{URL: "https://example.com/a.png", MimeType: "image/png"},
	}}})
	var unsupported *harness.CapabilityUnsupportedError
	if !errors.As(err, &unsupported) || !strings.Contains(err.Error(), "remote image URLs") {
		t.Fatalf("err = %v, want remote image URL rejection", err)
	}
}

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

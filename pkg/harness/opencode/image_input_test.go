package opencode

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
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

// promptStart runs one prompt turn and returns the `prompt` field of the
// start frame the bridge received.
func promptStart(t *testing.T, prompt harness.Prompt) (any, error) {
	t.Helper()
	const token = "image-token"
	got := make(chan map[string]any, 1)
	srv := newServer(t, token, func(turn *bridgetest.Turn, start map[string]any) {
		got <- start
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
		})
	})
	sandbox := newFakeSandbox(srv)
	h, err := CreateOpenCode(Settings{MintBridgeToken: func(string) string { return token }})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "image-session", SessionWorkDir: "/vercel/sandbox/image-session", SandboxSession: sandbox,
	})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
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

// TS: inline images reach the bridge as normalized text/image prompt parts.
func TestDoPromptTurn_InlineImagesSentAsPromptParts(t *testing.T) {
	raw, _ := base64.StdEncoding.DecodeString(tinyPNG)
	prompt, err := promptStart(t, harness.Prompt{Message: &types.Message{Role: types.RoleUser, Content: []types.ContentPart{
		types.TextContent{Text: "What is in this image?"},
		types.FileContent{MediaType: "image/png", Data: raw},
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

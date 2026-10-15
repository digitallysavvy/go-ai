package anthropic

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func messageIDs(chunks []*provider.StreamChunk) []string {
	var ids []string
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeResponseMetadata && c.ResponseMetadata != nil && c.ResponseMetadata.ID != "" {
			ids = append(ids, c.ResponseMetadata.ID)
		}
	}
	return ids
}

// anthropic-message-lifecycle.test.ts, "fails when a different message starts
// while the previous message is open".
func TestFixtureSplicedMessageStart(t *testing.T) {
	m, closeFn := fixtureModel(t, serveChunksFixture("spliced-message-start", t))
	defer closeFn()
	s, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}, Tools: []types.Tool{{Name: "test-tool", Parameters: map[string]interface{}{"type": "object"}}}})
	if err != nil {
		t.Fatal(err)
	}
	chunks := drain(t, s)
	errs := 0
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeError:
			errs++
			var inv *providererrors.InvalidResponseDataError
			if !errors.As(c.Err, &inv) {
				t.Fatalf("error = %T %v", c.Err, c.Err)
			}
		case provider.ChunkTypeToolCall:
			if c.ToolCall.ID == "toolu_second" {
				t.Fatal("tool call from the spliced message leaked")
			}
		case provider.ChunkTypeFinish:
			t.Fatal("finish after a spliced message")
		}
	}
	if ids := strings.Join(messageIDs(chunks), ","); errs != 1 || ids != "msg_first" {
		t.Fatalf("errors=%d ids=%s", errs, ids)
	}
}

// "ignores a duplicate message_start for the already-open message".
func TestFixtureDuplicateMessageStart(t *testing.T) {
	chunks := streamFixture(t, "duplicate-message-start")
	finishes := 0
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeError {
			t.Fatalf("error chunk: %v", c.Err)
		}
		if c.Type == provider.ChunkTypeFinish {
			finishes++
		}
	}
	if ids := messageIDs(chunks); len(ids) != 1 || finishes == 0 {
		t.Fatalf("ids=%v finishes=%d", ids, finishes)
	}
}

// TS fallback fixtures (generate, stream and the mid-output boundary).
func TestFixtureFallback(t *testing.T) {
	res := generateFixture(t, "anthropic-fallback", nil)
	if len(res.Content) != 2 {
		t.Fatalf("content = %+v", res.Content)
	}
	custom, ok := res.Content[0].(types.CustomContent)
	if !ok || custom.Kind != "anthropic.fallback" || string(custom.ProviderMetadata) != `{"anthropic":{"from":{"model":"claude-fable-5"},"to":{"model":"claude-opus-4-8"},"type":"fallback"}}` {
		t.Fatalf("custom = %+v", res.Content[0])
	}
	if *res.Usage.InputTokens != 412 || *res.Usage.OutputTokens != 264 {
		t.Fatalf("usage = %d/%d", *res.Usage.InputTokens, *res.Usage.OutputTokens)
	}

	chunks := streamFixture(t, "anthropic-fallback")
	found := false
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeCustom && c.CustomContent != nil && c.CustomContent.Kind == "anthropic.fallback" {
			found = true
		}
	}
	if !found {
		t.Fatal("no anthropic.fallback custom chunk")
	}
	var text strings.Builder
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeText {
			text.WriteString(c.Text)
		}
	}
	if text.String() != "The printing press was invented by Johannes Gutenberg around 1440." {
		t.Fatalf("text = %q", text.String())
	}
	want := `[{"inputTokens":408,"model":"claude-fable-5","outputTokens":0,"type":"message"},{"inputTokens":412,"model":"claude-opus-4-8","outputTokens":264,"type":"fallback_message"}]`
	if got := jsonString(t, anthropicMeta(t, finishOf(chunks).ProviderMetadata)["iterations"]); got != want {
		t.Fatalf("iterations\n got %s\nwant %s", got, want)
	}

	chunks = streamFixture(t, "anthropic-fallback-mid-output")
	endIdx, fbIdx, startIdx := -1, -1, -1
	for i, c := range chunks {
		switch {
		case c.Type == provider.ChunkTypeReasoningEnd && c.ID == "0" && endIdx < 0:
			endIdx = i
		case c.Type == provider.ChunkTypeCustom && c.CustomContent != nil && c.CustomContent.Kind == "anthropic.fallback":
			fbIdx = i
		case c.Type == provider.ChunkTypeReasoningStart && c.ID == "2":
			startIdx = i
		}
	}
	if endIdx < 0 || fbIdx <= endIdx || startIdx <= fbIdx {
		t.Fatalf("order: reasoning-end %d, fallback %d, reasoning-start %d", endIdx, fbIdx, startIdx)
	}
}

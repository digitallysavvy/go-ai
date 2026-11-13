package providerutils

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// JSON.parse reads a number out of the double range as Infinity; zod's
// object schemas strip it under an undeclared key, so TS keeps the frame
// (node: openaiChatChunkSchema.safeParse(JSON.parse(frame)).success).
// encoding/json failed the whole frame as a JSON parse error.
func TestR95carryChunkOutOfRangeNumber(t *testing.T) {
	frame := `{"choices":[{"index":0,"delta":{"content":"x"},"finish_reason":null}],"extra":1e400}`
	for _, shape := range []ChatChunkShape{{Kind: ChunkKindOpenAI}, {}, {Kind: ChunkKindMistral}} {
		if msg, err := CheckChatChunk(frame, shape); msg != "" || err != nil {
			t.Errorf("kind %d: %q %v", shape.Kind, msg, err)
		}
	}
	if msg, err := CheckTypedFrame(`{"type":"response.created","x":-1e400}`); msg != "" || err != nil {
		t.Errorf("typed frame: %q %v", msg, err)
	}
	// A frame that is valid JSON is never a JSONParseError (TS: a
	// TypeValidationError when its typed decode fails).
	_, err := ChunkParseFailure(`{"usage":{"prompt_tokens":1e400}}`, errors.New("decode"))
	var jsonErr *providererrors.JSONParseError
	var valErr *providererrors.ValidationError
	if errors.As(err, &jsonErr) || !errors.As(err, &valErr) {
		t.Errorf("ChunkParseFailure = %T %v, want a ValidationError", err, err)
	}
	// Syntax errors still are.
	if _, err := ChunkParseFailure(`{"a":`, nil); !errors.As(err, &jsonErr) {
		t.Errorf("syntax error = %T", err)
	}
}

// moonshotai-chat-api-types.ts tokenUsageSchema (TS fce7a32cfd):
// prompt_tokens_details.cache_write_tokens is z.number().nullish().
func TestR95carryMoonshotCacheWriteTokens(t *testing.T) {
	shape := ChatChunkShape{Kind: ChunkKindMoonshot}
	ok := `{"choices":[],"usage":{"prompt_tokens_details":{"cached_tokens":1,"cache_write_tokens":2}}}`
	if msg, err := CheckChatChunk(ok, shape); msg != "" || err != nil {
		t.Fatalf("valid frame: %q %v", msg, err)
	}
	bad := `{"choices":[],"usage":{"prompt_tokens_details":{"cache_write_tokens":"2"}}}`
	if msg, err := CheckChatChunk(bad, shape); msg == "" || err == nil {
		t.Error("a string cache_write_tokens passed the chunk schema")
	}
}

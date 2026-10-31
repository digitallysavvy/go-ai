package openai

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS throwIfOpenAIStreamErrorBeforeOutput keeps peeking until a chunk with
// non-empty content, tool calls or annotations (isOpenAIChatOutputChunk). The
// first chunk OpenAI sends, {role:"assistant",content:""}, is not output, so an
// error frame right after it still fails the call before any chunk is handed
// out, which is what lets the call be retried.
func TestChatStreamErrorAfterEmptyRoleChunkFailsBeforeOutput(t *testing.T) {
	sseData := `data: {"id":"c1","model":"gpt-4o","created":1,"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}

data: {"error":{"message":"overloaded","type":"server_error","code":"server_error"}}

data: [DONE]

`
	stream := newOpenAIStream(io.NopCloser(strings.NewReader(sseData)), true)
	defer stream.Close() //nolint:errcheck

	chunk, err := stream.Next()
	if err == nil {
		t.Fatalf("first Next returned chunk %#v, want the stream error", chunk)
	}
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) || providerErr.StatusCode != 500 {
		t.Fatalf("error = %T %[1]v, want ProviderError with status 500", err)
	}
}

// A finish_reason-only chunk and a usage-only chunk are not output either.
func TestChatStreamErrorAfterFinishOnlyChunkFailsBeforeOutput(t *testing.T) {
	sseData := `data: {"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: {"error":{"message":"bad","type":"server_error"}}

`
	stream := newOpenAIStream(io.NopCloser(strings.NewReader(sseData)), false)
	defer stream.Close() //nolint:errcheck

	if chunk, err := stream.Next(); err == nil {
		t.Fatalf("first Next returned chunk %#v, want the stream error", chunk)
	}
}

// Once a chunk with content arrives, everything held back (raw, response
// metadata, the empty text delta) is released in order, and a later error is an
// error part.
func TestChatStreamHeldChunksReleasedInOrderOnFirstOutput(t *testing.T) {
	sseData := `data: {"id":"c1","model":"gpt-4o","created":1,"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}

data: {"choices":[{"index":0,"delta":{"content":"hi"}}]}

data: {"error":{"message":"overloaded","type":"server_error"}}

data: [DONE]

`
	stream := newOpenAIStream(io.NopCloser(strings.NewReader(sseData)), true)
	defer stream.Close() //nolint:errcheck

	var got []string
	for {
		chunk, err := stream.Next()
		if err != nil {
			if err != io.EOF {
				t.Fatalf("Next error: %v", err)
			}
			break
		}
		got = append(got, string(chunk.Type)+":"+chunk.Text)
	}
	want := []string{
		string(provider.ChunkTypeRaw) + ":",
		string(provider.ChunkTypeResponseMetadata) + ":",
		string(provider.ChunkTypeText) + ":",
		string(provider.ChunkTypeRaw) + ":",
		string(provider.ChunkTypeText) + ":hi",
		string(provider.ChunkTypeRaw) + ":",
		string(provider.ChunkTypeError) + ":overloaded",
		string(provider.ChunkTypeFinish) + ":",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("chunks =\n%v\nwant\n%v", got, want)
	}
}

// A stream that ends without any output chunk is handed out whole (TS returns
// the stream when the peek reaches the end).
func TestChatStreamWithoutOutputIsDeliveredAtEnd(t *testing.T) {
	sseData := `data: {"id":"c1","model":"gpt-4o","created":1,"choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":"stop"}]}

data: [DONE]

`
	stream := newOpenAIStream(io.NopCloser(strings.NewReader(sseData)), false)
	defer stream.Close() //nolint:errcheck

	var types []provider.ChunkType
	for {
		chunk, err := stream.Next()
		if err != nil {
			if err != io.EOF {
				t.Fatalf("Next error: %v", err)
			}
			break
		}
		types = append(types, chunk.Type)
	}
	if len(types) == 0 || types[len(types)-1] != provider.ChunkTypeFinish {
		t.Fatalf("chunk types = %v, want a trailing finish", types)
	}
}

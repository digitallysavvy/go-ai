package openai

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// drainChatTypes reads a chat stream to the end and returns its chunk types
// and the first Next error other than io.EOF.
func drainChatTypes(s *openAIStream) ([]provider.ChunkType, error) {
	defer s.Close() //nolint:errcheck
	var got []provider.ChunkType
	for {
		c, err := s.Next()
		if err != nil {
			if err == io.EOF {
				return got, nil
			}
			return got, err
		}
		got = append(got, c.Type)
	}
}

// TS isOpenAIChatOutputChunk counts tool-call deltas and annotations as output,
// so an error frame after them is an error part, not a failed call.
func TestChatStreamToolCallOrAnnotationDeltaEndsErrorPeek(t *testing.T) {
	cases := map[string]string{
		"tool_calls":  `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"f","arguments":""}}]}}]}`,
		"annotations": `{"choices":[{"index":0,"delta":{"annotations":[{"type":"url_citation","url_citation":{"start_index":0,"end_index":0,"url":"https://a.test","title":"A"}}]}}]}`,
	}
	for name, first := range cases {
		t.Run(name, func(t *testing.T) {
			sse := "data: " + first + "\n\ndata: {\"error\":{\"message\":\"boom\",\"type\":\"server_error\"}}\n\ndata: [DONE]\n\n"
			got, err := drainChatTypes(newOpenAIStream(io.NopCloser(strings.NewReader(sse)), false))
			if err != nil {
				t.Fatalf("stream failed with %v (chunks %v), want an error part", err, got)
			}
			found := false
			for _, ct := range got {
				if ct == provider.ChunkTypeError {
					found = true
				}
			}
			if !found {
				t.Fatalf("chunks = %v, want an error part", got)
			}
		})
	}
}

// A chunk that fails to parse ends the TS peek, so a later error frame is an
// error part and the chunks before it are handed out.
func TestChatStreamUnparsableChunkEndsErrorPeek(t *testing.T) {
	sse := "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}]}\n\n" +
		"data: {not json\n\n" +
		"data: {\"error\":{\"message\":\"boom\",\"type\":\"server_error\"}}\n\ndata: [DONE]\n\n"
	got, err := drainChatTypes(newOpenAIStream(io.NopCloser(strings.NewReader(sse)), false))
	if err != nil {
		t.Fatalf("stream failed with %v (chunks %v), want error parts", err, got)
	}
	n := 0
	for _, ct := range got {
		if ct == provider.ChunkTypeError {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("chunks = %v, want 2 error parts (parse failure, error frame)", got)
	}
}

type dataThenErrReader struct {
	data string
	err  error
}

func (r *dataThenErrReader) Read(p []byte) (int, error) {
	if r.data == "" {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// The OpenAI-compatible chat model has no early-error peek: chunks are handed
// out as they arrive (a reasoning delta is not output for the native peek), so
// a transport error after the first chunk still leaves that chunk delivered.
func TestCompatChatStreamDoesNotHoldChunks(t *testing.T) {
	readErr := errors.New("connection reset")
	body := &dataThenErrReader{
		data: "data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"r\"}}]}\n\n",
		err:  readErr,
	}
	s := newOpenAIStream(io.NopCloser(body), false)
	s.compat = true
	got, err := drainChatTypes(s)
	if !errors.Is(err, readErr) {
		t.Fatalf("err = %v, want the read error", err)
	}
	if len(got) == 0 {
		t.Fatalf("no chunk delivered before the read error; compat streams must not hold chunks")
	}
}

// Before the first output event, a chunk that fails to parse (or fails the
// event schema, or is Chat Completions shaped) hands the stream out like TS:
// the held response.created chunks come first, in arrival order.
func TestResponsesStreamDecodeFailureReleasesHeldChunksFirst(t *testing.T) {
	created := `data: {"type":"response.created","response":{"id":"r1","model":"m","created_at":1}}` + "\n\n"
	cases := map[string]string{
		"unparsable": "data: {not json\n\n",
		"schema":     `data: {"type":"response.output_text.delta","item_id":"m1","output_index":0,"content_index":0,"delta":5}` + "\n\n",
		"chat-shape": `data: {"id":"x","choices":[{"index":0,"delta":{"content":"hi"}}]}` + "\n\n",
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := drainResponsesTypeLabels(created + bad)
			if err != nil {
				t.Fatalf("Next error: %v (chunks %v)", err, got)
			}
			if len(got) < 4 {
				t.Fatalf("chunks = %v, want raw, response-metadata, raw, error first", got)
			}
			want := []string{
				string(provider.ChunkTypeRaw) + ":response.created",
				string(provider.ChunkTypeResponseMetadata),
			}
			if got[0] != want[0] || got[1] != want[1] || got[3] != string(provider.ChunkTypeError) {
				t.Fatalf("chunks = %v, want %v then raw, error", got, want)
			}
		})
	}
}

// A response.created that arrives after output still delivers its metadata.
func TestResponsesStreamLateCreatedDeliversMetadata(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r1","model":"m","created_at":1}}

data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"m1"}}

data: {"type":"response.output_text.delta","item_id":"m1","output_index":0,"content_index":0,"delta":"hi"}

data: {"type":"response.created","response":{"id":"r2","model":"m","created_at":2}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m1"}}

data: {"type":"response.completed","response":{"id":"r2","usage":{"input_tokens":1,"output_tokens":1}}}

`
	got, err := drainResponsesRawLabels(t, sse)
	if err != nil {
		t.Fatalf("Next error: %v", err)
	}
	n := 0
	for _, l := range got {
		if l == string(provider.ChunkTypeResponseMetadata) {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("chunks = %v, want 2 response-metadata chunks", got)
	}
}

// drainResponsesTypeLabels is drainResponsesRawLabels for payloads whose raw
// value may have no "type" (a raw part then has the bare "raw" label).
func drainResponsesTypeLabels(sse string) ([]string, error) {
	s := newResponsesStream(io.NopCloser(strings.NewReader(sse)), true)
	defer s.Close() //nolint:errcheck
	var got []string
	for {
		c, err := s.Next()
		if err != nil {
			if err == io.EOF {
				return got, nil
			}
			return got, err
		}
		label := string(c.Type)
		if m, ok := c.Raw.(map[string]interface{}); ok && c.Type == provider.ChunkTypeRaw {
			if typ, ok := m["type"].(string); ok {
				label += ":" + typ
			}
		}
		got = append(got, label)
	}
}

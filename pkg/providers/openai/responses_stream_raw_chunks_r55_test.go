package openai

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// drainResponsesRawLabels returns "<chunk type>[:<raw event type>]" for every chunk.
func drainResponsesRawLabels(t *testing.T, sse string) ([]string, error) {
	t.Helper()
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
		if c.Type == provider.ChunkTypeRaw {
			if m, ok := c.Raw.(map[string]interface{}); ok {
				label += ":" + m["type"].(string)
			}
		}
		got = append(got, label)
	}
}

// TS emits one raw part per SSE event, ahead of the parts the event produces,
// for every event: the early response.created / response.in_progress ones and
// unmodeled event types included.
func TestResponsesStreamRawChunkOncePerEvent(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r1","model":"m","created_at":1}}

data: {"type":"response.in_progress","response":{"id":"r1","created_at":1,"model":"m"}}

data: {"type":"response.some_new_event","x":1}

data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"m1"}}

data: {"type":"response.output_text.delta","item_id":"m1","output_index":0,"content_index":0,"delta":"hi"}

data: {"type":"response.some_new_event","x":2}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m1"}}

data: {"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":1,"output_tokens":1}}}

`
	got, err := drainResponsesRawLabels(t, sse)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"raw:response.created",
		"response-metadata",
		"raw:response.in_progress",
		"raw:response.some_new_event",
		"raw:response.output_item.added",
		"text-start",
		"raw:response.output_text.delta",
		"text",
		"raw:response.some_new_event",
		"raw:response.output_item.done",
		"text-end",
		"raw:response.completed",
		"finish",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("chunks =\n%v\nwant\n%v", got, want)
	}
}

// An error frame in the early-error window fails the call with nothing
// delivered, raw chunks included.
func TestResponsesStreamErrorAfterInProgressDeliversNothing(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r1","model":"m","created_at":1}}

data: {"type":"response.in_progress","response":{"id":"r1","created_at":1,"model":"m"}}

data: {"type":"error","sequence_number":1,"code":"server_error","message":"boom"}

`
	got, err := drainResponsesRawLabels(t, sse)
	if err == nil {
		t.Fatalf("expected the stream error, got chunks %v", got)
	}
	if len(got) != 0 {
		t.Fatalf("chunks before the error = %v, want none", got)
	}
}

// A stream that ends inside the early-error window is handed out whole.
func TestResponsesStreamEndsBeforeOutputKeepsHeldChunks(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r1","model":"m","created_at":1}}

data: {"type":"response.in_progress","response":{"id":"r1","created_at":1,"model":"m"}}

`
	got, err := drainResponsesRawLabels(t, sse)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"raw:response.created", "response-metadata", "raw:response.in_progress", "finish"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("chunks =\n%v\nwant\n%v", got, want)
	}
}

// response.failed without an error object is not an early error: the held
// chunks, its raw part and the finish are delivered.
func TestResponsesStreamFailedWithoutErrorObjectKeepsRawChunks(t *testing.T) {
	sse := `data: {"type":"response.created","response":{"id":"r1","model":"m","created_at":1}}

data: {"type":"response.failed","sequence_number":1,"response":{"id":"r1","incomplete_details":null,"usage":null,"service_tier":null}}

`
	got, err := drainResponsesRawLabels(t, sse)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"raw:response.created", "response-metadata", "raw:response.failed", "finish"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("chunks =\n%v\nwant\n%v", got, want)
	}
}

// TS reports providerMetadata.openai.responseId from response.created only; a
// stream that never sends it reports null, whatever id the terminal event holds.
func TestResponsesStreamFinishResponseIDNullWithoutCreated(t *testing.T) {
	sse := `data: {"type":"response.completed","response":{"id":"r_late","usage":{"input_tokens":1,"output_tokens":1}}}

`
	s := newResponsesStream(io.NopCloser(strings.NewReader(sse)), false)
	defer s.Close() //nolint:errcheck
	c, err := s.Next()
	if err != nil {
		t.Fatal(err)
	}
	if c.Type != provider.ChunkTypeFinish || !strings.Contains(string(c.ProviderMetadata), `"responseId":null`) {
		t.Fatalf("finish = %s, want responseId null", c.ProviderMetadata)
	}
}

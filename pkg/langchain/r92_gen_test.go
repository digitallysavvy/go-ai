package langchain

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

// Round 92 gen tests (@langchain/langgraph-sdk 1.14.0 utils/sse.js,
// client/base.js; @langchain/langgraph 1.7.1 pregel/remote.js).

func r92genEvents(t *testing.T, stream string) ([]StreamEvent, error) {
	t.Helper()
	var events []StreamEvent
	for ev := range readRemoteGraphEvents(context.Background(), io.NopCloser(strings.NewReader(stream))) {
		if ev.Err != nil {
			return events, ev.Err
		}
		events = append(events, ev)
	}
	return events, nil
}

// BytesLineDecoder ends a line at CR, LF or CRLF; SSEDecoder drops a leading
// BOM and decodes empty or whitespace-only data as null.
func TestR92genSSEDecoding(t *testing.T) {
	events, err := r92genEvents(t, "\xef\xbb\xbfevent: values\rdata: {\"a\":1}\r\revent: values\r\n\r\nevent: values\ndata:  \n\nevent: messages\ndata: [1,\ndata: 2]")
	if err != nil {
		t.Fatal(err)
	}
	want := []StreamEvent{
		{Mode: "values", Data: map[string]interface{}{"a": float64(1)}},
		{Mode: "values", Data: nil},
		{Mode: "values", Data: nil},
		{Mode: "messages", Data: []interface{}{float64(1), float64(2)}},
	}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

// SSEDecoder JSON.parse throws on invalid data, which fails the stream.
func TestR92genSSEInvalidJSONFails(t *testing.T) {
	events, err := r92genEvents(t, "event: values\ndata: {\"a\":1}\n\nevent: values\ndata: not json\n\nevent: values\ndata: {}\n\n")
	if err == nil || len(events) != 1 {
		t.Fatalf("events = %v, err = %v; want one event, then an error", events, err)
	}
}

// RemoteGraph._streamIterator without subgraphs: a namespaced event is
// yielded only for messages; "updates|ns" and "error|ns" are matched by
// prefix (remote.js:355-360).
func TestR92genRemoteGraphNamespacedEvents(t *testing.T) {
	events, err := r92genEvents(t, "event: values|sub:1\ndata: {\"x\":1}\n\nevent: messages|sub:1\ndata: [2]\n\nevent: updates|sub:1\ndata: {\"n\":{}}\n\nevent: metadata\ndata: {}\n\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []StreamEvent{{Mode: "messages", Data: []interface{}{float64(2)}}}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}

	_, err = r92genEvents(t, "event: updates|sub:1\ndata: {\"__interrupt__\":[{\"value\":\"q\"}]}\n\n")
	var interrupt *GraphInterruptError
	if !errors.As(err, &interrupt) {
		t.Fatalf("namespaced interrupt: err = %v", err)
	}
	_, err = r92genEvents(t, "event: error|sub:1\ndata: \"boom\"\n\n")
	if err == nil || err.Error() != "boom" {
		t.Fatalf("namespaced error: err = %v", err)
	}
}

package langchain

import (
	"context"
	"io"
	"strings"
	"testing"
)

// langgraph-sdk decodes run stream event data with plain JSON.parse
// (utils/sse.js decodeArraysToJson), so a __proto__ key is an ordinary
// property and the values event reaches the adapter.
func TestR98genRemoteGraphEventDataPlainJSON(t *testing.T) {
	sse := "event: values\ndata: {\"__proto__\":{\"a\":1},\"messages\":[]}\n\n"
	var got []StreamEvent
	for ev := range readRemoteGraphEvents(context.Background(), io.NopCloser(strings.NewReader(sse))) {
		got = append(got, ev)
	}
	if len(got) != 1 || got[0].Err != nil || got[0].Mode != "values" {
		t.Fatalf("events = %#v", got)
	}
	data, _ := got[0].Data.(map[string]interface{})
	if proto, ok := data["__proto__"].(map[string]interface{}); !ok || proto["a"] != float64(1) {
		t.Fatalf("data = %#v", got[0].Data)
	}
}

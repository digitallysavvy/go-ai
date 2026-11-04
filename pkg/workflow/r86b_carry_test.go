package workflow

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// TS parseInt(response.headers.get('x-workflow-stream-tail-index'), 10):
// Headers.get decodes the header bytes as Latin-1, so a raw 0xA0 byte is
// U+00A0 and parseInt skips it; a UTF-8 encoded U+00A0 is "Â " (NaN).
// Recorded with Node 24 fetch against a raw TCP server: a0 35 -> 5,
// a0 a0 2d 33 -> -3, c2 a0 38 -> NaN.
func TestR86bcarryTailIndexLatin1Whitespace(t *testing.T) {
	for _, c := range []struct {
		header string
		want   string
	}{
		{"\xa0" + "5", "?startIndex=1"},
		{"\xa0\xa0-3", "?startIndex=0"},
		{"\xa0" + "40", "?startIndex=36"},
		{"\xc2\xa0" + "8", "?startIndex=0"},
	} {
		srv := newScriptedServer(t, scriptedResponse{header: map[string]string{"x-workflow-stream-tail-index": c.header}}, scriptedResponse{frames: []string{`{"type":"finish"}`}})
		_ = captureLog(t)
		tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.srv.URL + "/api/chat", InitialStartIndex: -5})
		reconnect(t, tr, ai.ChatTransportReconnectToStreamRequest{ChatID: "c"})
		want := []string{"/api/chat/c/stream?startIndex=-5", "/api/chat/c/stream" + c.want}
		if got := srv.requested(); !reflect.DeepEqual(got, want) {
			t.Errorf("tail %q: requests = %v, want %v", c.header, got, want)
		}
		if n, ok := parseIntPrefix(c.header); c.header == "\xc2\xa08" && ok {
			t.Errorf("parseIntPrefix(%q) = %d, true; want NaN", c.header, n)
		}
	}
}

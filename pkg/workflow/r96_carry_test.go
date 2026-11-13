package workflow

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// TS posts JSON.stringify(body): a non-finite number (provider metadata
// JSON.parse read as Infinity) is null, and <, > and & stay literal;
// json.Marshal failed the request and escaped HTML.
func TestR96carryChatTransportBodyIsJSONStringify(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Workflow-Run-ID", "run-1")
		_, _ = io.WriteString(w, "data: {\"type\":\"finish\"}\n\n")
	}))
	defer srv.Close()
	extra := map[string]interface{}{"meta": map[string]interface{}{"score": math.Inf(1)}, "note": "<a&b>"}

	tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL, MaxConsecutiveErrors: 1})
	chunks, errs := tr.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{ChatID: "c", Body: extra})
	for range chunks {
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	mux := NewWorkflowRunMultiplexer(WorkflowRunMultiplexerOptions{API: srv.URL})
	if _, err := mux.SendMessages(context.Background(), SendMessagesOptions{Body: extra}); err != nil {
		t.Fatalf("multiplexer send: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("bodies = %q", bodies)
	}
	for _, b := range bodies {
		if !strings.Contains(b, `"meta":{"score":null}`) || !strings.Contains(b, `"note":"<a&b>"`) {
			t.Errorf("body = %s", b)
		}
	}
}

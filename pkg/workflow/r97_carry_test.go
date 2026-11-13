package workflow

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

// TS WorkflowChatTransport reads frames with parseJsonEventStream
// (safeParseJSON + uiMessageChunkSchema). Node (provider-utils / ai at
// 2ebc78b654), getErrorMessage of the failed chunk:
//   - `{"__proto__":{"a":1},"type":"x"}` -> `AI_JSONParseError: JSON parsing
//     failed: Text: {"__proto__":{"a":1},"type":"x"}.\nError message:
//     SyntaxError: Object contains forbidden prototype property`
//   - `null` -> `AI_TypeValidationError: Type validation failed: Value:
//     null.\nError message: ...` (zod's issue list, known difference)
//
// Go decoded both with encoding/json: the __proto__ chunk passed through and
// null became a nil chunk sent downstream.
func TestR97carryWorkflowChatTransportRejectsLikeSafeParseJSON(t *testing.T) {
	for _, tc := range []struct {
		name, frame, want string
	}{
		{"proto", `{"__proto__":{"a":1},"type":"x"}`, "Failed to reconnect after 1 consecutive errors. Last error: AI_JSONParseError: JSON parsing failed: Text: {\"__proto__\":{\"a\":1},\"type\":\"x\"}.\nError message: SyntaxError: Object contains forbidden prototype property"},
		{"null", `null`, "Failed to reconnect after 1 consecutive errors. Last error: AI_TypeValidationError: Type validation failed: Value: null.\nError message: Invalid input: expected object, received null"},
		{"number", `5`, "Failed to reconnect after 1 consecutive errors. Last error: AI_TypeValidationError: Type validation failed: Value: 5.\nError message: Invalid input: expected object, received number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: " + tc.frame + "\n\n"))
			}))
			defer srv.Close()
			tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL, MaxConsecutiveErrors: 1})
			out, errs := tr.ReconnectToStream(context.Background(), ai.ChatTransportReconnectToStreamRequest{ChatID: "c"})
			chunks, err := collectChunks(out, errs, 3*time.Second)
			if len(chunks) != 0 {
				t.Fatalf("chunks = %v, want none", chunks)
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err = %v\nwant %s", err, tc.want)
			}
		})
	}
}

// JSON.parse reads 1e400 as Infinity (ruling 4); encoding/json failed the
// whole frame. Invalid UTF-8 decodes like TextDecoderStream (one U+FFFD per
// maximal subpart).
func TestR97carryWorkflowChatTransportReadsLikeJSONParse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"data-x\",\"data\":{\"n\":1e400,\"s\":\"a\xF0\x9F\x98b\"}}\n\ndata: {\"type\":\"finish\"}\n\n"))
	}))
	defer srv.Close()
	tr := NewWorkflowChatTransport(WorkflowChatTransportOptions{API: srv.URL})
	out, errs := tr.ReconnectToStream(context.Background(), ai.ChatTransportReconnectToStreamRequest{ChatID: "c"})
	chunks, err := collectChunks(out, errs, 3*time.Second)
	if err != nil || len(chunks) != 2 {
		t.Fatalf("chunks = %v, err = %v", chunks, err)
	}
	data, _ := chunks[0]["data"].(map[string]interface{})
	if n, _ := data["n"].(float64); !math.IsInf(n, 1) {
		t.Fatalf("n = %v, want +Inf", data["n"])
	}
	if s := data["s"]; s != "a�b" {
		t.Fatalf("s = %q, want %q", s, "a�b")
	}
}

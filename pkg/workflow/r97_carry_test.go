package workflow

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

// TS toUIMessageChunk passes providerMetadata as the object it is; Go reads
// the raw metadata, where a number beyond the double range is +Inf (ruling 4)
// instead of dropping the whole metadata.
func TestR97carryToUIMessageChunkMetadataInfinity(t *testing.T) {
	c, ok := ToUIMessageChunk(ModelCallStreamPart{Type: provider.ChunkTypeText, ID: "t", Text: "x", ProviderMetadata: []byte(`{"p":{"n":1e400}}`)})
	if !ok {
		t.Fatal("no chunk")
	}
	md, _ := c["providerMetadata"].(map[string]interface{})
	p, _ := md["p"].(map[string]interface{})
	if n, _ := p["n"].(float64); !math.IsInf(n, 1) {
		t.Fatalf("providerMetadata = %v, want p.n = +Inf", c["providerMetadata"])
	}
}

// OnAbort steps carry the deprecated ResponseMessages alias like the run's
// own step results (agent step loop sets it to Response.Messages).
func TestR97carryOnAbortStepsKeepResponseMessagesAlias(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var steps []types.StepResult
	a, err := NewWorkflowAgent(WorkflowAgent{
		Model: &r96carryAbortModel{},
		Tools: []types.Tool{{Name: "t1", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			cancel()
			return "done", nil
		}}},
		OnAbort: func(_ context.Context, s []types.StepResult) { mu.Lock(); steps = s; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.GenerateWithOptions(ctx, WorkflowGenerateOptions{Prompt: "p"}); err == nil {
		t.Fatal("want an abort error")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(steps) != 1 || len(steps[0].Response.Messages) == 0 || len(steps[0].ResponseMessages) != len(steps[0].Response.Messages) { //nolint:staticcheck // the deprecated alias under test
		t.Fatalf("steps = %+v", steps)
	}
}

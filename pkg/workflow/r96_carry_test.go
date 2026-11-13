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
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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

// r96carryAbortModel calls a tool on its first step (with provider metadata)
// and blocks until the context ends on the second.
type r96carryAbortModel struct{ wfMockModel }

func (m *r96carryAbortModel) DoGenerate(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.calls++
	if m.calls == 1 {
		return &types.GenerateResult{
			FinishReason:     types.FinishReasonToolCalls,
			ToolCalls:        []types.ToolCall{{ID: "c1", ToolName: "t1", Arguments: map[string]interface{}{}}},
			ProviderMetadata: map[string]interface{}{"mock": map[string]interface{}{"k": "v"}},
		}, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

// TS onAbort({steps}) hands over the finished steps' full StepResults.
func TestR96carryOnAbortStepsAreFullStepResults(t *testing.T) {
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
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	s := steps[0]
	if len(s.Content) == 0 || len(s.Response.Messages) == 0 || s.ProviderMetadata["mock"] == nil {
		t.Errorf("step lacks content/response/provider metadata: content=%d response.messages=%d metadata=%v", len(s.Content), len(s.Response.Messages), s.ProviderMetadata)
	}
}

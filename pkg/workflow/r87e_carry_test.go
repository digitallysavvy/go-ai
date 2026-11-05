package workflow

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r87ecarryCallIDs records the call ID of every root start event.
type r87ecarryCallIDs struct {
	telemetry.NoopTelemetryIntegration
	mu  sync.Mutex
	ids []string
}

func (c *r87ecarryCallIDs) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	c.mu.Lock()
	c.ids = append(c.ids, e.CallID)
	c.mu.Unlock()
	return ctx
}

// TS workflow-agent-telemetry.ts gives a durable run's root, model steps and
// report the ID createIdGenerator({prefix: 'call', size: 24}) generates.
func TestR87ecarryWorkflowRootCallIDFormat(t *testing.T) {
	rec := &r87ecarryCallIDs{}
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model:     &wfMockModel{},
		Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}
	res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^call-[0-9A-Za-z]{24}$`)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.ids) != 2 {
		t.Fatalf("root starts = %d, want 2 (%v)", len(rec.ids), rec.ids)
	}
	for _, id := range rec.ids {
		if !re.MatchString(id) {
			t.Errorf("root call ID %q, want call- and 24 alphanumerics", id)
		}
	}
	if rec.ids[0] == rec.ids[1] {
		t.Errorf("two runs share the call ID %q", rec.ids[0])
	}
}

// r87ecarryTwoStepModel streams a tool call, then reasoning and text with
// provider metadata and detailed usage.
type r87ecarryTwoStepModel struct {
	wfMockModel
	mu    sync.Mutex
	calls int
}

func (m *r87ecarryTwoStepModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	m.mu.Lock()
	m.calls++
	n := m.calls
	m.mu.Unlock()
	in, out, cacheRead := int64(10), int64(5), int64(4)
	if n == 1 {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "tc1", ToolName: "t1", Arguments: map[string]interface{}{"q": "x"}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: &types.Usage{InputTokens: &in, OutputTokens: &out}},
		}), nil
	}
	return testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeReasoning, Reasoning: "thinking"},
		{Type: provider.ChunkTypeText, Text: "done"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop,
			Usage:            &types.Usage{InputTokens: &in, OutputTokens: &out, InputDetails: &types.InputTokenDetails{CacheReadTokens: &cacheRead}},
			ProviderMetadata: json.RawMessage(`{"mock":{"final":true}}`)},
	}), nil
}

func (m *r87ecarryTwoStepModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.mu.Lock()
	m.calls++
	n := m.calls
	m.mu.Unlock()
	in, out, cacheRead := int64(10), int64(5), int64(4)
	if n == 1 {
		return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "tc1", ToolName: "t1", Arguments: map[string]interface{}{"q": "x"}}},
			Usage: types.Usage{InputTokens: &in, OutputTokens: &out}}, nil
	}
	return &types.GenerateResult{
		Content:          []types.ContentPart{types.ReasoningContent{Text: "thinking"}, types.TextContent{Text: "done"}},
		Text:             "done",
		FinishReason:     types.FinishReasonStop,
		Usage:            types.Usage{InputTokens: &in, OutputTokens: &out, InputDetails: &types.InputTokenDetails{CacheReadTokens: &cacheRead}},
		ProviderMetadata: map[string]interface{}{"mock": map[string]interface{}{"final": true}},
	}, nil
}

// TS reports a durable WorkflowAgent run's root end with withFinalStepFields
// (workflow-agent-telemetry.ts): the OTel integrations then record the final
// step's reasoning and provider metadata, every step's tool calls, and the
// detailed usage on the ai.workflowAgent.stream root. Go sent only the finish
// reason, text and token totals.
func TestR87ecarryWorkflowRootEndCarriesFinalStepFields(t *testing.T) {
	for _, mode := range []string{"generate", "stream"} {
		t.Run(mode, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: &r87ecarryTwoStepModel{},
				Tools: []types.Tool{{
					Name: "t1",
					Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
						return "ok", nil
					},
				}},
				Telemetry: &telemetry.Options{
					IsEnabled:    telemetry.Bool(true),
					Integrations: []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("wf")})},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "generate" {
				if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"}); err != nil {
					t.Fatal(err)
				}
			} else {
				res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := res.ReadAll(); err != nil {
					t.Fatal(err)
				}
			}
			attrs := map[string]string{}
			found := false
			for _, s := range rec.Ended() {
				if s.Name() != workflowRootOperation {
					continue
				}
				found = true
				for _, kv := range s.Attributes() {
					attrs[string(kv.Key)] = kv.Value.Emit()
				}
			}
			if !found {
				t.Fatal("no ended ai.workflowAgent.stream root")
			}
			if got := attrs["ai.response.text"]; got != "done" {
				t.Errorf("ai.response.text = %q, want done", got)
			}
			if got := attrs["ai.response.reasoning"]; got != "thinking" {
				t.Errorf("ai.response.reasoning = %q, want the final step's reasoning", got)
			}
			if got := attrs["ai.response.toolCalls"]; !strings.Contains(got, `"toolCallId":"tc1"`) {
				t.Errorf("ai.response.toolCalls = %q, want the first step's call", got)
			}
			if got := attrs["ai.response.providerMetadata"]; got != `{"mock":{"final":true}}` {
				t.Errorf("ai.response.providerMetadata = %q, want the final step's", got)
			}
			if got := attrs["ai.usage.cachedInputTokens"]; got != "4" {
				t.Errorf("ai.usage.cachedInputTokens = %q, want 4 (detailed usage)", got)
			}
		})
	}
}

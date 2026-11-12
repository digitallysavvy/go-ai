package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 91 (stream).

// core1-A01 (TS set-abort-timeout.ts:27-36 labels any non-null timeoutMs;
// merge-abort-signals.ts:17 AbortSignal.timeout(0)): a configured zero or
// negative timeout has the TS text, not the Go fallback.
func TestR91streamZeroTimeoutText(t *testing.T) {
	zero, neg := time.Duration(0), -5*time.Millisecond
	tc := &TimeoutConfig{PerStep: &zero, FirstChunk: &zero, PerChunk: &neg}
	for reason, want := range map[TimeoutReason]string{
		TimeoutReasonStep:       "Step timeout of 0ms exceeded",
		TimeoutReasonFirstChunk: "First chunk timeout of 0ms exceeded",
		TimeoutReasonChunk:      "Chunk timeout of -5ms exceeded",
	} {
		if got := tc.timeoutError(reason, context.DeadlineExceeded).Error(); got != want {
			t.Errorf("%s: %q, want %q", reason, got, want)
		}
	}
	if got := NewToolTimeoutError(0, context.DeadlineExceeded).Error(); got != "The operation was aborted due to timeout" {
		t.Errorf("tool 0: %q", got)
	}
	// An unconfigured duration keeps the Go text.
	if got := (&TimeoutError{Reason: TimeoutReasonStep, Err: context.DeadlineExceeded}).Error(); got != "step timeout exceeded: context deadline exceeded" {
		t.Errorf("unset: %q", got)
	}

	// StreamText with PerStep 0 on a blocking stream: the abort reason.
	var mu sync.Mutex
	var reason error
	model := r90streamModel(func(ctx context.Context) provider.TextStream {
		return &r90streamBlocking{ctx: ctx}
	})
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "go", Timeout: &TimeoutConfig{PerStep: &zero},
		OnAbortEvent: func(_ context.Context, e GenerateTextAbortEvent) { mu.Lock(); reason = e.Reason; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	r91streamDrain(res.FullStream())
	mu.Lock()
	defer mu.Unlock()
	if reason == nil || reason.Error() != "Step timeout of 0ms exceeded" {
		t.Fatalf("abort reason = %v", reason)
	}
}

func r91streamDrain(s provider.TextStream) []provider.StreamChunk {
	var out []provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil || c == nil {
			return out
		}
		out = append(out, *c)
	}
}

// r91streamMetaTypes runs ToUIMessageStream with a MessageMetadata that
// returns {t: part type} and returns the part types it saw and the
// message-metadata chunks' t values.
func r91streamMetaTypes(t *testing.T, r *StreamTextResult) (seen, written []string) {
	t.Helper()
	var mu sync.Mutex
	chunks, errs := r.ToUIMessageStream(context.Background(), UIMessageStreamResultOptions{
		MessageMetadata: func(part map[string]interface{}) map[string]interface{} {
			mu.Lock()
			defer mu.Unlock()
			typ, _ := part["type"].(string)
			seen = append(seen, typ)
			return map[string]interface{}{"t": typ}
		},
		OnError: func(err error) string { return err.Error() },
	})
	for c := range chunks {
		if c["type"] == "message-metadata" {
			m, _ := c["messageMetadata"].(map[string]interface{})
			tv, _ := m["t"].(string)
			written = append(written, tv)
		}
	}
	for range errs {
	}
	mu.Lock()
	defer mu.Unlock()
	return seen, written
}

// core1-B01 (TS to-ui-message-stream.ts:128 messageMetadata({part}) with
// the TextStreamPart): a failed tool result is a tool-error part.
func TestR91streamUIMessageMetadataToolError(t *testing.T) {
	failing := types.Tool{Name: "t", Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return nil, errors.New("boom")
		}}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool_calls", Usage: r71streamUsage},
		}),
		Prompt: "x", Tools: []types.Tool{failing},
	})
	if err != nil {
		t.Fatal(err)
	}
	seen, written := r91streamMetaTypes(t, r)
	got := strings.Join(seen, ",")
	if !strings.Contains(got, "tool-call,tool-error") || strings.Contains(got, "tool-result") {
		t.Fatalf("metadata part types = %s", got)
	}
	if !strings.Contains(strings.Join(written, ","), "tool-error") {
		t.Fatalf("message-metadata chunks = %v", written)
	}
}

// core1-B02 (TS harness-stream-text-result.ts:250 enqueues no
// response-metadata part): the Go-only markers of an external parts stream
// do not reach MessageMetadata.
func TestR91streamUIMessageMetadataSkipsInternalMarkers(t *testing.T) {
	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStart},
		{Type: provider.ChunkTypeStartStep},
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "hi"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "r1"}},
		{Type: provider.ChunkTypeFirstChunk},
		{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop, Usage: r71streamUsage},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r71streamUsage},
	})
	r := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{TerminalFinishClosesTurn: true})
	seen, written := r91streamMetaTypes(t, r)
	for _, typ := range append(seen, written...) {
		if typ == string(provider.ChunkTypeResponseMetadata) || typ == string(provider.ChunkTypeFirstChunk) {
			t.Fatalf("internal marker reached MessageMetadata: seen %v, written %v", seen, written)
		}
	}
	if !strings.Contains(strings.Join(seen, ","), "text-end,finish-step") {
		t.Fatalf("seen = %v", seen)
	}
}

// core1-S01 (TS stream-language-model-call.ts:866-873): a forwarded
// tool-input-start carries the tool's title and metadata, and so does the
// UI tool-input-start chunk.
func TestR91streamToolInputStartToolFields(t *testing.T) {
	steps := func() []provider.StreamChunk {
		return []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "c1", ToolName: "weather", Title: "provider title"}},
			{Type: provider.ChunkTypeToolInputDelta, ID: "c1", Text: "{}"},
			{Type: provider.ChunkTypeToolInputEnd, ID: "c1"},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "weather", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool_calls", Usage: r71streamUsage},
		}
	}
	tool := types.Tool{Name: "weather", Title: "Weather", Metadata: map[string]interface{}{"k": "v"}, Parameters: map[string]interface{}{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "sunny", nil
		}}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, steps()), Prompt: "x", Tools: []types.Tool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	var start *types.ToolCall
	for _, c := range r91streamDrain(r.FullStream()) {
		if c.Type == provider.ChunkTypeToolInputStart {
			start = c.ToolCall
		}
	}
	if start == nil || start.Title != "Weather" || start.ToolMetadata["k"] != "v" {
		t.Fatalf("tool-input-start = %+v", start)
	}

	r, err = StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, steps()), Prompt: "x", Tools: []types.Tool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	chunks, errs := r.ToUIMessageStream(context.Background(), UIMessageStreamResultOptions{})
	var ui UIMessageChunk
	for c := range chunks {
		if c["type"] == "tool-input-start" {
			ui = c
		}
	}
	for range errs {
	}
	meta, _ := ui["toolMetadata"].(map[string]interface{})
	if ui["title"] != "Weather" || meta["k"] != "v" {
		t.Fatalf("UI tool-input-start = %v", ui)
	}
}

// core1-A03 (TS stream-text.ts 1739-1820): a delta or end of an ended
// block is forwarded, followed by an error part "<kind> part <id> not
// found", and not recorded. TS (node at 9f7a842ae1): text "ab".
func TestR91streamOrphanBlockDelta(t *testing.T) {
	var mu sync.Mutex
	var onChunkErrs, onErrs int
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeTextStart, ID: "0"},
			{Type: provider.ChunkTypeText, ID: "0", Text: "a"},
			{Type: provider.ChunkTypeTextEnd, ID: "0"},
			{Type: provider.ChunkTypeText, ID: "0", Text: "late"},
			{Type: provider.ChunkTypeTextStart, ID: "0"},
			{Type: provider.ChunkTypeText, ID: "0", Text: "b"},
			{Type: provider.ChunkTypeTextEnd, ID: "0"},
			{Type: provider.ChunkTypeReasoningStart, ID: "r"},
			{Type: provider.ChunkTypeReasoningEnd, ID: "r"},
			{Type: provider.ChunkTypeReasoning, ID: "r", Text: "x"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage},
		}),
		Prompt: "x",
		OnChunk: func(c provider.StreamChunk) {
			if c.Type == provider.ChunkTypeError {
				mu.Lock()
				onChunkErrs++
				mu.Unlock()
			}
		},
		OnError: func(context.Context, error) { mu.Lock(); onErrs++; mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	var seq []string
	for _, c := range r91streamDrain(r.FullStream()) {
		switch c.Type {
		case provider.ChunkTypeText:
			seq = append(seq, "delta:"+c.Text)
		case provider.ChunkTypeReasoning:
			seq = append(seq, "reasoning:"+c.Text)
		case provider.ChunkTypeError:
			seq = append(seq, "error:"+c.Err.Error())
		}
	}
	want := "delta:a,delta:late,error:text part 0 not found,delta:b,reasoning:x,error:reasoning part r not found"
	if got := strings.Join(seq, ","); got != want {
		t.Fatalf("parts\n got %s\nwant %s", got, want)
	}
	text, err := r.ReadAll()
	if err != nil || text != "ab" {
		t.Fatalf("text = %q, %v", text, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if onChunkErrs != 0 || onErrs != 0 {
		t.Fatalf("orphan error reached OnChunk %d / OnError %d times", onChunkErrs, onErrs)
	}
	// A delta of a block that was never started stays accepted.
	r, err = StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeText, ID: "", Text: "plain"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage},
		}),
		Prompt: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if text, err := r.ReadAll(); err != nil || text != "plain" {
		t.Fatalf("never-started text = %q, %v", text, err)
	}
}

// core1-D01 (TS stream-text.ts:3854-3866 parseOutput passes step.response
// and step.usage of the final step): a failed output parse reports the final
// step's usage and response, not the multi-step total.
func TestR91streamOutputParseFinalStepContext(t *testing.T) {
	big := &types.Usage{InputTokens: r71streamInt64(100), OutputTokens: r71streamInt64(1), TotalTokens: r71streamInt64(101)}
	model := r71streamModel(nil,
		[]provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}, RawArguments: "{}"}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, RawFinishReason: "tool_calls", Usage: big},
		},
		[]provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "final-id", ModelID: "m"}},
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "not json"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage},
		})
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: model, Prompt: "x", Tools: r90streamTools,
		StopWhen: []StopCondition{StepCountIs(2)},
		Output:   ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})}),
	})
	if err != nil {
		t.Fatal(err)
	}
	r91streamDrain(r.FullStream())
	var noObj *NoObjectGeneratedError
	if !errors.As(r.OutputErr(), &noObj) {
		t.Fatalf("OutputErr = %v", r.OutputErr())
	}
	if noObj.Usage == nil || noObj.Usage.InputTokens == nil || *noObj.Usage.InputTokens != 1 {
		t.Fatalf("usage = %+v", noObj.Usage)
	}
	if noObj.Response == nil || noObj.Response.ID != "final-id" || noObj.FinishReason != types.FinishReasonStop {
		t.Fatalf("response = %+v, finish %q", noObj.Response, noObj.FinishReason)
	}
}

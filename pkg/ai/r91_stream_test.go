package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
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

type r91streamTelRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu    sync.Mutex
	start []telemetry.TelemetryStartEvent
	steps []telemetry.TelemetryStepStartEvent
}

func (r *r91streamTelRecorder) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.start = append(r.start, e)
	return ctx
}

func (r *r91streamTelRecorder) OnStepStart(ctx context.Context, e telemetry.TelemetryStepStartEvent) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, e)
	return ctx
}

// core2-05 / core1-R02 call sites (TS legacy-open-telemetry.ts:358-365,
// 654-660; standardize-prompt.ts): StreamText gives the root start event the
// string prompt and every step start event the step's system text, only
// when inputs are recorded.
func TestR91streamTelemetryPromptFields(t *testing.T) {
	run := func(record bool, prompt string, messages []types.Message) *r91streamTelRecorder {
		rec := &r91streamTelRecorder{}
		settings := &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{rec}}
		if !record {
			f := false
			settings.RecordInputs = &f
		}
		r, err := StreamText(context.Background(), StreamTextOptions{
			Model:     r71streamModel(nil, r71streamToolCallStep([2]string{"call-1", "value"}), r71streamHelloWorld),
			System:    "be nice",
			Prompt:    prompt,
			Messages:  messages,
			Tools:     []types.Tool{r71streamTool1(true, nil, nil)},
			StopWhen:  []StopCondition{StepCountIs(3)},
			Telemetry: settings,
		})
		if err != nil {
			t.Fatal(err)
		}
		r91streamDrain(r.FullStream())
		return rec
	}
	rec := run(true, "hi", nil)
	if len(rec.start) != 1 || rec.start[0].PromptString == nil || *rec.start[0].PromptString != "hi" {
		t.Fatalf("start PromptString = %+v", rec.start)
	}
	if len(rec.steps) != 2 {
		t.Fatalf("step starts = %d", len(rec.steps))
	}
	for i, e := range rec.steps {
		if e.System != "be nice" {
			t.Errorf("step %d System = %q", i, e.System)
		}
	}
	rec = run(true, "", []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}})
	if rec.start[0].PromptString != nil {
		t.Fatalf("messages input has PromptString %q", *rec.start[0].PromptString)
	}
	rec = run(false, "hi", nil)
	if rec.start[0].PromptString != nil || rec.steps[0].System != "" || rec.steps[1].System != "" {
		t.Fatalf("inputs recorded with RecordInputs false: %+v / %+v", rec.start[0].PromptString, rec.steps)
	}
}

// TS 8585d7e56f (to-ui-message-stream.test.ts "routes an invalid tool call
// through onError once" / "forwards tool errors for valid tool calls").
func TestR91streamUIInvalidToolCallOnErrorOnce(t *testing.T) {
	invalidErr := errors.New("invalid tool input")
	run := func(parts []provider.StreamChunk) ([]UIMessageChunk, []error) {
		src := testutil.NewMockTextStream(parts)
		r := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{})
		var mu sync.Mutex
		var seen []error
		chunks, errs := r.ToUIMessageStream(context.Background(), UIMessageStreamResultOptions{
			SendStart: r91streamFalse(), SendFinish: r91streamFalse(),
			OnError: func(err error) string {
				mu.Lock()
				seen = append(seen, err)
				mu.Unlock()
				return "formatted tool error"
			},
		})
		var out []UIMessageChunk
		for c := range chunks {
			if c["type"] != "start-step" && c["type"] != "finish-step" {
				out = append(out, c)
			}
		}
		for range errs {
		}
		mu.Lock()
		defer mu.Unlock()
		return out, seen
	}
	got, seen := run([]provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "test-tool", Arguments: map[string]interface{}{"value": "invalid"}, Invalid: true, Dynamic: true, Error: invalidErr}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "test-tool", Input: map[string]interface{}{"value": "invalid"}, Error: errors.New("Error: invalid tool input"), Dynamic: true}},
	})
	r71streamAssertJSON(t, "invalid call", got, `[
		{"dynamic":true,"errorText":"formatted tool error","input":{"value":"invalid"},"toolCallId":"call-1","toolName":"test-tool","type":"tool-input-error"}]`)
	if len(seen) != 1 || !errors.Is(seen[0], invalidErr) {
		t.Fatalf("onError calls = %v", seen)
	}
	got, seen = run([]provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "test-tool", Arguments: map[string]interface{}{"value": "valid"}, Dynamic: true}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "test-tool", Input: map[string]interface{}{"value": "valid"}, Error: errors.New("execution failed"), Dynamic: true}},
	})
	r71streamAssertJSON(t, "valid call", got, `[
		{"dynamic":true,"input":{"value":"valid"},"toolCallId":"call-1","toolName":"test-tool","type":"tool-input-available"},
		{"dynamic":true,"errorText":"formatted tool error","toolCallId":"call-1","type":"tool-output-error"}]`)
	if len(seen) != 1 {
		t.Fatalf("onError calls = %v", seen)
	}
}

func r91streamFalse() *bool { f := false; return &f }

// TS 11323f9894 (validate-ui-messages.test.ts "invalid messages and parts",
// "invalid JSON values in metadata"): a part is checked only against the
// schema its type / state selects, validation stops at the first invalid
// element, and the error stays bounded.
func TestR91streamValidateUIMessagesBounded(t *testing.T) {
	validate := func(raw string) error {
		res := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: raw})
		if res.Success {
			return nil
		}
		return res.Error
	}
	for name, tc := range map[string]struct{ raw, want string }{
		"unknown type": {`[{"id":"1","role":"user","parts":[{"type":"unknown"}]}]`, "[0].parts[0].type: " + uiPartTypeMessage},
		"missing type": {`[{"id":"1","role":"user","parts":[{"type":"text","text":"valid"},{},{}]}]`, "[0].parts[1].type: " + uiPartTypeMessage},
		"tool state":   {`[{"id":"1","role":"assistant","parts":[{"type":"tool-foo","toolCallId":"1","state":"unknown"}]}]`, "[0].parts[0].state: " + uiToolStateMessage},
		"text field":   {`[{"id":"1","role":"user","parts":[{"type":"text"}]}]`, "[0].parts[0]: Invalid input: expected string for text"},
		"first msg":    {`[{"id":"1","role":"user","parts":[{"type":"text","text":"valid"}]},{},{}]`, "[1].id: Invalid input: expected string for id"},
		"non-finite":   {`[{"id":"1","role":"user","parts":[{"type":"text","text":"Hello","providerMetadata":{"provider":{"key":[1e999]}}}]}]`, uiInvalidJSONValueMessage},
	} {
		err := validate(tc.raw)
		var verr *providererrors.ValidationError
		if !errors.As(err, &verr) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v (%T), want %q", name, err, err, tc.want)
		}
	}

	parts := make([]string, 2500)
	for i := range parts {
		parts[i] = "{}"
	}
	raw := `[{"id":"1","role":"user","parts":[` + strings.Join(parts, ",") + `]}]`
	err := validate(raw)
	if err == nil || len(err.Error()) > len(raw)+1000 {
		t.Fatalf("2500 malformed parts: error length %d", len(err.Error()))
	}

	// A Go value with a non-finite number fails validation with that issue.
	msgs := []UIMessage{{ID: "1", Role: "user", Parts: []UIMessagePart{&TextUIPart{Text: "Hello",
		ProviderMetadata: map[string]interface{}{"provider": map[string]interface{}{"key": []interface{}{math.Inf(1)}}}}}}}
	err = r91streamValidate(msgs)
	var verr *providererrors.ValidationError
	if !errors.As(err, &verr) || !strings.Contains(err.Error(), uiInvalidJSONValueMessage) {
		t.Fatalf("Go Inf: err = %v (%T)", err, err)
	}
}

func r91streamValidate(msgs []UIMessage) error {
	res := SafeValidateUIMessages(context.Background(), ValidateUIMessagesOptions{Messages: msgs})
	if res.Success {
		return nil
	}
	return res.Error
}

// r91streamCountingSchema validates against a JSON schema and counts calls.
type r91streamCountingSchema struct {
	inner schema.Validator
	mu    sync.Mutex
	calls int
}

func (s *r91streamCountingSchema) Validator() schema.Validator { return s }
func (s *r91streamCountingSchema) JSONSchema() map[string]interface{} {
	return s.inner.JSONSchema()
}
func (s *r91streamCountingSchema) Validate(v interface{}) error {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return s.inner.Validate(v)
}

func r91streamArrayParser(t *testing.T, elementSchema map[string]interface{}) (*arrayOutputStreamParser, *r91streamCountingSchema) {
	t.Helper()
	cs := &r91streamCountingSchema{inner: schema.NewJSONSchema(elementSchema)}
	out := ArrayOutput[interface{}](ArrayOutputOptions[interface{}]{ElementSchema: cs})
	p := newArrayOutputStreamParser(context.Background(), out.(outputProcessor))
	if p == nil {
		t.Fatal("no array stream parser for ArrayOutput")
	}
	cs.mu.Lock()
	cs.calls = 0
	cs.mu.Unlock()
	return p, cs
}

// TS 91a67c2f56 util/array-output-stream.test.ts: nested values and escaped
// strings in chunks of 1, 2, 7 and 10000 characters; each element is
// validated once and snapshots only grow.
func TestR91streamArrayStreamParserChunks(t *testing.T) {
	values := []interface{}{
		map[string]interface{}{"nested": []interface{}{`a,]}\"`, map[string]interface{}{"number": -1.25e30}}, "unicode": "中文🌍"},
		[]interface{}{false, nil, []interface{}{float64(1), float64(2)}},
		-12.5e-3,
		`escaped \ and " quotes`,
		true,
		nil,
	}
	b, _ := json.Marshal(map[string]interface{}{"elements": values})
	text := []rune(string(b))
	for _, size := range []int{1, 2, 7, 10000} {
		p, cs := r91streamArrayParser(t, map[string]interface{}{})
		var last []interface{}
		prev := -1
		for i := 0; i < len(text); i += size {
			end := i + size
			if end > len(text) {
				end = len(text)
			}
			partial, publish, fallback := p.feed(context.Background(), string(text[i:end]))
			if fallback {
				t.Fatalf("size %d: fallback at %d", size, i)
			}
			if publish {
				last = partial.([]interface{})
				if len(last) <= prev {
					t.Fatalf("size %d: snapshot length %d after %d", size, len(last), prev)
				}
				prev = len(last)
			}
		}
		gotJSON, _ := json.Marshal(last)
		wantJSON, _ := json.Marshal(values)
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("size %d: %s, want %s", size, gotJSON, wantJSON)
		}
		if cs.calls != len(values) {
			t.Fatalf("size %d: %d validations, want %d", size, cs.calls, len(values))
		}
	}
}

func TestR91streamArrayStreamParserWithholdAndFallback(t *testing.T) {
	ctx := context.Background()
	p, _ := r91streamArrayParser(t, map[string]interface{}{"type": "number"})
	steps := []struct {
		delta   string
		publish bool
		want    string
	}{
		{`{"elements":[1,`, true, `[]`},
		{`2`, true, `[1]`},
		{`]`, false, ``},
		{`}`, true, `[1,2]`},
	}
	for _, s := range steps {
		partial, publish, fallback := p.feed(ctx, s.delta)
		if fallback || publish != s.publish {
			t.Fatalf("%q: publish %v fallback %v", s.delta, publish, fallback)
		}
		if publish {
			if b, _ := json.Marshal(partial); string(b) != s.want {
				t.Fatalf("%q: %s, want %s", s.delta, b, s.want)
			}
		}
	}
	for _, text := range []string{
		`{"other":[],"elements":[1]}`,
		`{"elem\u0065nts":[1]}`,
		`{"elements":[1,]}`,
		`{"elements":[1e,2]}`,
		`{"elements":[1],"elements":[2]}`,
		`{"elements":[1]} trailing`,
	} {
		p, _ := r91streamArrayParser(t, map[string]interface{}{"type": "number"})
		if _, _, fallback := p.feed(ctx, text); !fallback {
			t.Errorf("%s: no fallback", text)
		}
	}
	// Invalid elements are skipped.
	p, _ = r91streamArrayParser(t, map[string]interface{}{"type": "number"})
	partial, publish, fallback := p.feed(ctx, `{"elements":[1,"invalid",2]}`)
	if b, _ := json.Marshal(partial); fallback || !publish || string(b) != `[1,2]` {
		t.Fatalf("skip invalid: %s %v %v", b, publish, fallback)
	}
	// Non-array outputs get no incremental parser.
	if newArrayOutputStreamParser(ctx, ObjectOutput[map[string]interface{}](ObjectOutputOptions{Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})}).(outputProcessor)) != nil {
		t.Fatal("object output got an array parser")
	}
}

// StreamText with ArrayOutput streams elements validated once each and the
// same partials as the stateless parser; a nonstandard envelope falls back.
func TestR91streamArrayOutputStreamText(t *testing.T) {
	run := func(text string, piece int) (string, int) {
		cs := &r91streamCountingSchema{inner: schema.NewJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"n": map[string]interface{}{"type": "number"}}, "required": []interface{}{"n"}})}
		chunks := []provider.StreamChunk{{Type: provider.ChunkTypeStreamStart}, {Type: provider.ChunkTypeTextStart, ID: "t"}}
		for i := 0; i < len(text); i += piece {
			end := i + piece
			if end > len(text) {
				end = len(text)
			}
			chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeText, ID: "t", Text: text[i:end]})
		}
		chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "t"},
			provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage})
		r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, chunks), Prompt: "x",
			Output: ArrayOutput[map[string]interface{}](ArrayOutputOptions[map[string]interface{}]{ElementSchema: cs})})
		if err != nil {
			t.Fatal(err)
		}
		r91streamDrain(r.FullStream())
		b, _ := json.Marshal(r.PartialOutput())
		cs.mu.Lock()
		defer cs.mu.Unlock()
		return string(b), cs.calls
	}
	elems := make([]string, 20)
	for i := range elems {
		elems[i] = fmt.Sprintf(`{"n":%d}`, i)
	}
	text := `{"elements":[` + strings.Join(elems, ",") + `]}`
	last, calls := run(text, 3)
	if !strings.HasPrefix(last, `[{"n":0},`) || !strings.HasSuffix(last, `{"n":19}]`) {
		t.Fatalf("partial = %s", last)
	}
	// Each element once while streaming, plus the final output's own
	// validation of every element (TS: the same counts).
	if calls != 40 {
		t.Fatalf("validations = %d, want 40", calls)
	}
	// Another envelope falls back to the stateless parser.
	if last, _ = run(`{"x":1,"elements":[{"n":1},{"n":2}]}`, 4); last != `[{"n":1},{"n":2}]` {
		t.Fatalf("fallback partial = %s", last)
	}
}
